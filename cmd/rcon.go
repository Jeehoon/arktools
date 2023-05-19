/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// rconCmd represents the rcon command
var rconCmd = &cobra.Command{
	Use:   "rcon",
	Short: "RCON ARK Server",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()
		cobra.CheckErr(doRCON(ctx, args))
	},
}

func init() {
	cobra.OnInitialize(setDefaults)

	rootCmd.AddCommand(rconCmd)

	rconCmd.Flags().String("rcon-addr", "", "RCON Address")
	rconCmd.Flags().String("password", "", "RCON Password")

	cobra.CheckErr(viper.BindPFlags(rconCmd.Flags()))
}

type RconHeader struct {
	Len     uint32
	Xid     uint32
	ReqType uint32
}

var xid = uint32(100)

func rconTalk(conn net.Conn, reqType uint32, payload []byte) (resp []byte, err error) {

	msg := &RconHeader{}

	// send
	msg.Len = uint32(len(payload) + 10)
	msg.Xid = xid
	msg.ReqType = reqType
	xid++

	if err := binary.Write(conn, binary.LittleEndian, msg); err != nil {
		return nil, errors.Wrapf(err, "rcon write header")
	}

	body := append(payload, 0x00, 0x00)
	if _, err := conn.Write(body); err != nil {
		return nil, errors.Wrapf(err, "rcon write body")
	}

	// recv
	if err := binary.Read(conn, binary.LittleEndian, msg); err != nil {
		return nil, errors.Wrapf(err, "rcon read header")
	}

	buff := make([]byte, msg.Len-8)
	if _, err := io.ReadFull(conn, buff); err != nil {
		return nil, errors.Wrapf(err, "rcon read body")
	}

	buff = buff[:len(buff)-2]
	return buff, nil
}

func doRCON(ctx context.Context, args []string) (err error) {

	addr := viper.GetString("rcon-addr")
	password := viper.GetString("password")

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return errors.Wrapf(err, "net.Dial(%v)", addr)
	}

	if resp, err := rconTalk(conn, 3, []byte(password)); err != nil {
		return errors.Wrap(err, "sendMessage auth")
	} else {
		if _, err := Output.Write(resp); err != nil {
			return errors.Wrap(err, "Output.Write")
		}
	}

	command := strings.Join(args, " ")

	if resp, err := rconTalk(conn, 2, []byte(command)); err != nil {
		return errors.Wrap(err, "sendMessage command")
	} else {
		resp = append(resp, '\n')
		if _, err := Output.Write(resp); err != nil {
			return errors.Wrap(err, "Output.Write")
		}
	}

	return nil
}

/*
class ArkRcon(object):
  def __init__(self, ip, port, password):
    self.sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    self.sock.connect((ip, port))
    self.xid = 0
    self.auth(password)

  def send_message(self, reqtype, data):
    self.xid += 1
    msg = struct.pack('<III', len(data) + 10, self.xid, reqtype).decode('utf-8') + data + "\0\0";
    b = msg.encode()
    self.sock.send(b)
    return self.xid

  def recv_message(self):
    b = self.sock.recv(12)
    size, resid, restype =  struct.unpack('<III', b)
    data = self.sock.recv(size)[:-2].decode('utf-8')
    return resid, restype, data

  def auth(self,password):
    reqid = self.send_message(3, password)
    resid, restype, data = self.recv_message()
    if resid == -1 or resid == 0xffffffff:
      raise Exception('ArkRcon', 'auth: Authentication failed')

  def talk(self, message):
    reqid = self.send_message(2, message)
    while True:
      resid, restype, data = self.recv_message()
      if reqid == resid: break

    print(data)


*/
