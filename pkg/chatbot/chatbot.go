package chatbot

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/jeehoon/arktools/pkg/log"
	"github.com/pkg/errors"
)

type ChatBot struct {
	apiToken   string
	webdisAddr string
	clusterId  string
	channelId  string
	format     string

	client *http.Client
	dg     *discordgo.Session
}

func New(discordApiToken, webdisAddr, clusterId, channelId, format string) *ChatBot {
	if format == "" {
		format = "```md\n[%v][%v][%v]: %v\n```"
	}

	cb := new(ChatBot)
	cb.apiToken = discordApiToken
	cb.webdisAddr = webdisAddr
	cb.clusterId = clusterId
	cb.channelId = channelId
	cb.format = format

	cb.client = new(http.Client)
	return cb
}

func (cb *ChatBot) Serve(ctx context.Context) (err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	cb.dg, err = discordgo.New("Bot " + cb.apiToken)
	if err != nil {
		return errors.Wrap(err, "discordgo.New")
	}
	cb.dg.AddHandler(cb.MessageHandler)
	cb.dg.Identify.Intents = discordgo.IntentGuildMessages

	if err := cb.dg.Open(); err != nil {
		return errors.Wrap(err, "dg.Open")
	}

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer cancel()
		log.Infof("Bot is now running. Press CTRL-C to exit.")
		sc := make(chan os.Signal, 1)
		signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
		<-sc
		cb.dg.Close()
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer cancel()
		if err := cb.PollWebdis(ctx); err != nil {
			log.Errorf("cb.PollWebdis failure: %v", err)
		}
	}()

	wg.Wait()
	return nil
}

func (cb *ChatBot) MessageHandler(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	if m.ChannelID != cb.channelId {
		return
	}

	//content := m.Content
	var nick = m.Member.Nick
	var tribe string
	var player string

	pos1 := strings.Index(nick, "[")
	pos2 := strings.Index(nick, "]")

	if pos1 != -1 && pos2 != -1 && pos1 < pos2 {
		tribe = nick[pos1+1 : pos2]
		player = nick[pos2+1:]
	} else {
		tribe = ""
		player = nick
	}

	cb.SendWebdis("ChatBot", "Discord", tribe, player, m.Content)
}

func (cb *ChatBot) ForwardDiscord(msg *RedisMessage) {
	cb.dg.ChannelMessageSend(
		cb.channelId,
		fmt.Sprintf(cb.format, msg.ServerName, msg.TribeName, msg.SurvivorName, msg.Message),
	)
}

func (cb *ChatBot) LPush(msg any) (err error) {
	b, err := json.Marshal(msg)
	if err != nil {
		return errors.Wrap(err, "json.Marshal")
	}

	j := string(b)
	data := fmt.Sprintf("LPUSH/%v/%v", cb.clusterId, url.QueryEscape(j))
	req, err := http.NewRequest("POST", cb.webdisAddr, strings.NewReader(data))
	if err != nil {
		return errors.Wrap(err, "http.NewRequest")
	}

	resp, err := cb.client.Do(req)
	if err != nil {
		return errors.Wrap(err, "client.Do")
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return errors.Wrap(err, "ioutil.ReadAll")
	}

	log.Infof("LPUSH => %v %v", resp.Status, string(body))
	return nil
}

func (cb *ChatBot) LTrim(start, end int) (err error) {
	data := fmt.Sprintf("LTRIM/%v/%v/%v", cb.clusterId, start, end)
	req, err := http.NewRequest("POST", cb.webdisAddr, strings.NewReader(data))
	if err != nil {
		return errors.Wrap(err, "http.NewRequest")
	}

	resp, err := cb.client.Do(req)
	if err != nil {
		return errors.Wrap(err, "client.Do")
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return errors.Wrap(err, "ioutil.ReadAll")
	}

	log.Infof("LTRIM => %v %v", resp.Status, string(body))
	return nil
}

type RedisMessage struct {
	SessionName  string    `json:"SessionName"`
	Color        []float64 `json:"Color"`
	Epoch        float64   `json:"Epoch"`
	Date         []string  `json:"Date"`
	Timestamp    []string  `json:"Timestamp"`
	ServerName   string    `json:"ServerName"`
	SurvivorName string    `json:"SurvivorName"`
	TribeName    string    `json:"TribeName"`
	Message      string    `json:"Message"`
}

func (cb *ChatBot) LRange(start, end int) (items []*RedisMessage, err error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%v/LRANGE/%v/%v/%v", cb.webdisAddr, cb.clusterId, start, end), nil)
	if err != nil {
		return nil, errors.Wrap(err, "http.NewRequest")
	}

	resp, err := cb.client.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "client.Do")
	}
	defer resp.Body.Close()

	// ERROR  {"LRANGE":[false,"ERR wrong number of arguments for 'lrange' command"]}
	// NORMAL {"LRANGE":["....","....."]}
	var result = map[string][]any{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, errors.Wrap(err, "json.Decode")
	}

	if _, has := result["LRANGE"]; !has {
		return nil, errors.Errorf("invalid result: LRANGE not exist")
	}

	arr := result["LRANGE"]
	if len(arr) == 2 {
		if b, ok := arr[0].(bool); ok && b == false {
			return nil, errors.Errorf("LRANGE failure: %v", arr[1])
		}
	}

	for _, item := range arr {
		r := strings.NewReader(item.(string))
		var msg *RedisMessage
		if err := json.NewDecoder(r).Decode(&msg); err != nil {
			return nil, errors.Wrap(err, "json.Decode")
		}
		items = append(items, msg)
	}

	//log.Infof("LRANGE => %v", resp.Status)
	return items, nil
}

func (cb *ChatBot) SendWebdis(session, server, tribe, player, content string) (err error) {
	now := time.Now().UTC()
	epoch := float64(now.UnixMicro()) / 1000000
	year := fmt.Sprintf("%04d", now.Year())
	month := fmt.Sprintf("%02d", now.Month())
	day := fmt.Sprintf("%02d", now.Day())
	hour := fmt.Sprintf("%02d", now.Hour())
	minute := fmt.Sprintf("%02d", now.Minute())
	second := fmt.Sprintf("%02d", now.Second())

	msg := &RedisMessage{
		SessionName:  session,
		Color:        []float64{0.5, 0.7, 1, 1},
		Epoch:        epoch,
		Date:         []string{year, month, day},
		Timestamp:    []string{hour, minute, second},
		ServerName:   server,
		SurvivorName: player,
		TribeName:    tribe,
		Message:      content,
	}

	if err := cb.LPush(msg); err != nil {
		return errors.Wrap(err, "cb.LPush")
	}

	if err := cb.LTrim(0, 9); err != nil {
		return errors.Wrap(err, "cb.LPush")
	}

	return nil
}

func (cb *ChatBot) PollWebdis(ctx context.Context) (err error) {
	var last float64

	for ctx.Err() == nil {
		time.Sleep(500 * time.Millisecond)

		msgs, err := cb.LRange(0, 19)
		if err != nil {
			return errors.Wrap(err, "cb.LRange")
		}

		if last == 0 {
			last = msgs[0].Epoch
			continue
		}

		for i := len(msgs) - 1; i >= 0; i-- {
			msg := msgs[i]
			if last >= msg.Epoch {
				continue
			}

			if msg.ServerName == "Discord" {
				continue
			}

			cb.ForwardDiscord(msgs[i])
			last = msg.Epoch
		}
	}
	return nil
}
