package steamcmd

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unicode"

	"github.com/creack/pty"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

type SteamCmd struct {
	exec       string
	installDir string
	output     io.Writer
}

func NewSteamCmd(exec, installDir string) *SteamCmd {
	return &SteamCmd{
		exec:       exec,
		installDir: installDir,
	}
}

func (steamcmd *SteamCmd) SetOutput(w io.Writer) {
	steamcmd.output = w
}

func (steamcmd *SteamCmd) SendUserf(f string, args ...any) {
	if steamcmd.output == nil {
		return
	}

	if _, err := fmt.Fprintf(steamcmd.output, f+"\n", args...); err != nil {
		log.Warnf("SendUserf failure: %v", err)
		return
	}
}

func (steamcmd *SteamCmd) Run(ctx context.Context, fn func(out string) (cmd string)) (err error) {
	teardown := false
	cmd := exec.CommandContext(ctx, steamcmd.exec)

	pty, tty, err := pty.Open()
	if err != nil {
		return errors.Wrap(err, "pty.Start")
	}

	defer func() {
		teardown = true
		pty.Close()
		tty.Close()
	}()

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:  true,
		Setctty: true,
	}
	cmd.Stdin = tty
	cmd.Stdout = tty
	cmd.Stderr = tty

	if err := cmd.Start(); err != nil {
		return errors.Wrap(err, "steamcmd.Start")
	}

	go func() {
		r := bufio.NewReader(pty)

		var prefix string
		var lines []string

		for {
			peek, err := r.Peek(6)
			if err != nil {
				if !teardown {
					log.Errorf("stdout.Peek failure: %v", err)
				}
				return
			}

			if string(peek) == "Steam>" {
				if _, err := r.Discard(6); err != nil {
					if !teardown {
						log.Errorf("stdout.Discard failure: %v", err)
					}
					return
				}

				outs := strings.Join(lines, "\n")
				cmd := fn(outs)
				lines = nil

				log.Infof("SteamCMD Send: %v", cmd)
				fmt.Fprintf(pty, "%v\n", cmd)
				continue
			}

			data, isPrefix, err := r.ReadLine()
			if err != nil {
				if !teardown {
					log.Errorf("stdout.Read failure: %v", err)
				}
				return
			}
			log.Debugf("SteamCMD OUT: %v", string(data))

			if isPrefix {
				prefix = string(data)
				continue
			}

			line := prefix + string(data)
			prefix = ""

			line = strings.TrimRightFunc(line, unicode.IsSpace)
			if line == "" {
				continue
			}

			lines = append(lines, line)
		}
	}()

	if err := cmd.Wait(); err != nil {
		return errors.Wrap(err, "cmd.Wait")
	}

	return nil
}

func (steamcmd *SteamCmd) getAppInfo(ctx context.Context, appId int) (info string, err error) {
	bAppInfoUpdate := false
	bAppInfoPrint := false

	if err := steamcmd.Run(ctx, func(out string) (cmd string) {
		switch {
		case !bAppInfoUpdate:
			bAppInfoUpdate = true
			return "app_info_update 1"
		case !bAppInfoPrint:
			bAppInfoPrint = true
			return fmt.Sprintf("app_info_print %v", appId)
		default:
			// remove head lines

			//  app_info_print 376030
			if idx := strings.Index(out, "\n"); idx != -1 {
				out = out[idx+1:]
			}

			//  AppID : 376030, change number : 18652314/4294967295, last change : Wed May  3 13:17:24 2023
			if idx := strings.Index(out, "\n"); idx != -1 {
				out = out[idx+1:]
			}

			info = out

			return "quit"
		}

	}); err != nil {
		return "", errors.Wrap(err, "steamcmd.Run")
	}

	return info, nil
}

func (steamcmd *SteamCmd) UpdateServer(ctx context.Context, appId int) (err error) {
	bInstallDir := false
	bLogon := false
	bDownload := false

	if err := steamcmd.Run(ctx, func(out string) (cmd string) {
		if idx := strings.Index(out, "Waiting for user info...OK"); idx != -1 {
			bLogon = true
		}

		if idx := strings.Index(out, fmt.Sprintf("Success! App '%v' fully installed.", appId)); idx != -1 {
			bDownload = true
		}

		switch {
		case !bInstallDir:
			bInstallDir = true
			return fmt.Sprintf("force_install_dir %v", steamcmd.installDir)
		case !bLogon:
			return "login anonymous"
		case !bDownload:
			bDownload = true
			return fmt.Sprintf("app_update %v validate", appId)
		default:
			return "quit"
		}

	}); err != nil {
		return errors.Wrap(err, "steamcmd.Run")
	}

	steamcmd.SendUserf("ARK Server was updated. (restart required)")
	return nil
}

func (steamcmd *SteamCmd) HasUpdate(ctx context.Context, appId int) (bool, error) {
	// read steam app info
	info, err := steamcmd.getAppInfo(ctx, appId)
	if err != nil {
		return false, errors.Wrapf(err, "steamcmd.getAppInfo(%v)", appId)
	}

	steamInfo := map[string]string{}
	steamPairs := ReadAcf(info)
	for _, pair := range steamPairs {
		// .376030.depots.1004.manifests.public 4660701598619066954
		arr := strings.Split(pair[0], ".")
		if arr[2] == "depots" && arr[4] == "manifests" && arr[5] == "public" {
			steamInfo[arr[3]] = pair[1]
		}
	}
	log.Debugf("Steam Depots: %q", steamInfo)

	// read local app info
	localAcf := filepath.Join(steamcmd.installDir, "steamapps", fmt.Sprintf("appmanifest_%v.acf", appId))
	b, err := ioutil.ReadFile(localAcf)
	if err != nil {
		return false, errors.Wrap(err, "ioutil.ReadFile(appmanifest)")
	}

	localInfo := map[string]string{}
	localPairs := ReadAcf(string(b))
	for _, pair := range localPairs {
		// .AppState.InstalledDepots.1006.manifest" "6912453647411644579"
		arr := strings.Split(pair[0], ".")
		if arr[1] == "AppState" && arr[2] == "InstalledDepots" && arr[4] == "manifest" {
			localInfo[arr[3]] = pair[1]
		}
	}
	log.Debugf("Local Depots: %q", localInfo)

	// compare
	hasUpdate := false
	for k, _ := range localInfo {
		if steamInfo[k] != localInfo[k] {
			hasUpdate = true
			break
		}
	}

	if hasUpdate {
		steamcmd.SendUserf("ARK Server update required")
	} else {
		steamcmd.SendUserf("ARK Server is up-to-date")
	}

	return hasUpdate, nil
}

func (steamcmd *SteamCmd) UpdateRequiredMods(ctx context.Context, appId int, modIds []int) (required []int, err error) {

	for _, modId := range modIds {
		// fetch from steam
		title, steamUpdated, err := steamcmd.getPublishedFileDetails(modId)
		if err != nil {
			return nil, errors.Wrapf(err, "steamcmd.GetPublishedFileDetails(%v)", modId)
		}

		//fetch from local
		yamlFile := filepath.Join(steamcmd.modsRoot(), fmt.Sprintf("%v.yaml", modId))
		_, localUpdated, err := steamcmd.readYaml(yamlFile)
		if os.IsNotExist(err) {
			log.Warnf("MOD[%v] is not exist yaml file", modId)
			localUpdated = 0
		} else if err != nil {
			return nil, errors.Wrapf(err, "open mod .yaml")
		}

		if steamUpdated == localUpdated {
			log.Infof("MOD[%v](%v) is up-to-date.", modId, title)
			steamcmd.SendUserf("ARK MOD[%v](%v) is up-to-date", modId, title)
		} else {
			required = append(required, modId)
			log.Infof("MOD[%v](%v) is update required.", modId, title)
			steamcmd.SendUserf("ARK MOD[%v](%v) update required", modId, title)
		}
	}

	return required, nil
}

func (steamcmd *SteamCmd) UpdateMods(ctx context.Context, appId int, modIds []int) (err error) {
	// download Mods
	bInstallDir := false
	bLogon := false

	modTitles := map[int]string{}
	for _, modId := range modIds {
		modTitle, _, err := steamcmd.getPublishedFileDetails(modId)
		if err != nil {
			return errors.Wrapf(err, "steamcmd.GetPublishedFileDetails(%v)", modId)
		}
		modTitles[modId] = modTitle
	}

	var downloadIds []int
	if err := steamcmd.Run(ctx, func(out string) (cmd string) {
		if idx := strings.Index(out, "Waiting for user info...OK"); idx != -1 {
			bLogon = true
		}

		if strings.Index(out, fmt.Sprintf("Success. Downloaded item %v", modIds[0])) != -1 {
			downloadIds = append(downloadIds, modIds[0])
			modIds = modIds[1:]
		}

		switch {
		case !bInstallDir:
			bInstallDir = true
			return fmt.Sprintf("force_install_dir %v", steamcmd.installDir)
		case !bLogon:
			return "login anonymous"
		case len(modIds) > 0:
			modId := modIds[0]
			modTitle := modTitles[modId]
			log.Infof("MOD[%v](%v) download...", modId, modTitle)
			return fmt.Sprintf("workshop_download_item %v %v", appId, modId)
		default:
			return "quit"
		}
	}); err != nil {
		return errors.Wrap(err, "steamcmd.Run")
	}

	// unpack & install
	for _, modId := range downloadIds {
		modTitle := modTitles[modId]
		log.Infof("MOD[%v](%v) unpack", modId, modTitle)
		if err := steamcmd.unpackMod(ctx, appId, modId); err != nil {
			return errors.Wrapf(err, "steamcmd.unpackMod(%v)", modId)
		}

		log.Infof("MOD[%v](%v) create .mod", modId, modTitle)
		if err := steamcmd.createDotMod(ctx, appId, modId, modTitle); err != nil {
			return errors.Wrapf(err, "steamcmd.createDotMod(%v)", modId)
		}

		log.Infof("MOD[%v](%v) install", modId, modTitle)
		if err := steamcmd.installMod(ctx, appId, modId); err != nil {
			return errors.Wrapf(err, "steamcmd.installMod(%v)", modId)
		}
		steamcmd.SendUserf("ARK MOD[%v](%v) was updated (restart required)", modId, modTitle)
	}

	return nil
}

func (steamcmd *SteamCmd) modPath(appId, modId int) string {
	return filepath.Join(steamcmd.installDir,
		"steamapps", "workshop", "content",
		fmt.Sprintf("%v", appId),
		fmt.Sprintf("%v", modId),
		"WindowsNoEditor")
}

func (steamcmd *SteamCmd) unpackMod(ctx context.Context, appId, modId int) (err error) {
	modPath := steamcmd.modPath(appId, modId)
	var zfiles []string

	if err := filepath.Walk(modPath, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			log.Errorf("[%v] access failure: %v", path, err)
			return err
		}

		if info.IsDir() {
			return nil
		}

		if filepath.Ext(path) != ".z" {
			return nil
		}

		zfiles = append(zfiles, path)

		return nil
	}); err != nil {
		return errors.Wrapf(err, "failpath.Walk(%v)", modPath)
	}

	for _, zfile := range zfiles {
		if err := steamcmd.unpackFile(ctx, zfile); err != nil {
			return errors.Wrapf(err, "steamcmd.unpackFile(%v)", zfile)
		}
	}

	return nil
}

func (steamcmd *SteamCmd) unpackFile(ctx context.Context, zfile string) (err error) {
	dest := zfile[:len(zfile)-2]
	sizePath := zfile + ".uncompressed_size"

	// read data
	data, err := ioutil.ReadFile(zfile)
	if err != nil {
		return errors.Wrapf(err, "ioutil.ReadFile(%v)", zfile)
	}

	// read size
	sdata, err := ioutil.ReadFile(sizePath)
	if err != nil {
		return errors.Wrapf(err, "ioutil.ReadFile(%v)", sizePath)
	}

	s := strings.TrimSpace(string(sdata))
	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return errors.Wrapf(err, "strconv.ParseInt(%v)", s)
	}
	size := int(i)

	// create dest
	w, err := os.Create(dest)
	if err != nil {
		return errors.Wrapf(err, "os.Create(%v)", dest)
	}
	defer w.Close()

	// parse header
	hdr := &struct {
		Magic       [8]byte
		ChunkSizeLo uint32
		ChunkSizeHi uint32
		ComprotoLo  uint32
		ComprotoHi  uint32
		UncomtotLo  uint32
		UncomtotHi  uint32
	}{}
	if err := binary.Read(bytes.NewBuffer(data[0:32]), binary.LittleEndian, hdr); err != nil {
		return errors.Wrap(err, "binary.Read(hdr)")
	}
	data = data[32:]

	if !bytes.Equal(hdr.Magic[:], []byte{0xC1, 0x83, 0x2A, 0x9E, 0x00, 0x00, 0x00, 0x00}) {
		return errors.Errorf("bad file magic")
	}

	var comprused uint32
	var chunks []uint32

	for comprused < hdr.ComprotoLo {
		chunkHdr := &struct {
			ComprsizeLo uint32
			ComprsizeHi uint32
			UncomsizeLo uint32
			UncomsizeHi uint32
		}{}

		if err := binary.Read(bytes.NewBuffer(data[0:16]), binary.LittleEndian, chunkHdr); err != nil {
			return errors.Wrap(err, "binary.Read(chunkHdr)")
		}
		data = data[16:]
		chunks = append(chunks, chunkHdr.ComprsizeLo)
		comprused += chunkHdr.ComprsizeLo
	}

	for _, chunk := range chunks {
		zr, err := zlib.NewReader(bytes.NewBuffer(data[0:chunk]))
		if err != nil {
			return errors.Wrap(err, "zlib.NewReader")
		}

		unpackData, err := io.ReadAll(zr)
		if err != nil {
			return errors.Wrap(err, "io.ReadAll")
		}

		if _, err := w.Write(unpackData); err != nil {
			return errors.Wrap(err, "dest.Write")
		}

		size -= len(unpackData)
		data = data[chunk:]
	}

	// validation
	if size != 0 {
		return errors.Errorf("[%v] unpack size is invalid. delta: %v", zfile, size)
	}

	if err := os.Remove(zfile); err != nil {
		return errors.Wrapf(err, "os.Remove(%v)", zfile)
	}

	if err := os.Remove(sizePath); err != nil {
		return errors.Wrapf(err, "os.Remove(%v)", sizePath)
	}

	return nil
}

func readUe4String(r io.Reader) (s string, nread int, err error) {
	var l uint32
	if err := binary.Read(r, binary.LittleEndian, &l); err != nil {
		return "", 0, errors.Wrap(err, "binary.Read(len)")
	}
	nread += 4 + int(l)

	if l <= 0 {
		return "", nread, nil
	}

	data := make([]byte, l)
	if _, err := r.Read(data); err != nil {
		return "", 0, errors.Wrap(err, "Read")
	}

	// remove null
	data = data[:len(data)-1]

	return string(data), nread, nil
}

func writeUe4String(w io.Writer, s string) (err error) {
	// append null
	s = s + "\x00"

	var l = uint32(len(s))
	if err := binary.Write(w, binary.LittleEndian, l); err != nil {
		return errors.Wrap(err, "binary.Write(len)")
	}

	if _, err := w.Write([]byte(s)); err != nil {
		return errors.Wrap(err, "Write")
	}
	return nil
}

func (steamcmd *SteamCmd) getPublishedFileDetails(modId int) (title string, updated int, err error) {
	resp, err := http.PostForm("http://api.steampowered.com/ISteamRemoteStorage/GetPublishedFileDetails/v1",
		url.Values{"itemcount": {"1"}, "publishedfileids[0]": {fmt.Sprintf("%v", modId)}})
	if err != nil {
		return "", 0, errors.Wrapf(err, "http.PostForm")
	}

	var v = &struct {
		Response struct {
			PublishedFileDetails []struct {
				Result      int    `json:"result"`
				Title       string `json:"title"`
				TimeUpdated int    `json:"time_updated"`
			} `json:"publishedfiledetails"`
		} `json:"response"`
	}{}

	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", 0, errors.Wrapf(err, "json.Decode")
	}
	resp.Body.Close()

	if len(v.Response.PublishedFileDetails) == 0 {
		return "", 0, errors.Errorf("get publish file detail failure: length 0")
	}

	if result := v.Response.PublishedFileDetails[0].Result; result != 1 {
		return "", 0, errors.Errorf("get publish file detail failure: result: %v", result)
	}

	modTitle := v.Response.PublishedFileDetails[0].Title
	modUpdated := v.Response.PublishedFileDetails[0].TimeUpdated
	log.Debugf("MOD[%v](%v) GetPublishedFileDetails updated:%v", modId, modTitle, modUpdated)

	return modTitle, modUpdated, nil
}

func (steamcmd *SteamCmd) readUpdatedFromAcf(appId, modId int) (updated int, err error) {
	acfPath := filepath.Join(steamcmd.installDir,
		"steamapps", "workshop", fmt.Sprintf("appworkshop_%v.acf", appId))

	b, err := ioutil.ReadFile(acfPath)
	if err != nil {
		return 0, errors.Wrap(err, "ioutil.ReadFile")
	}

	pairs := ReadAcf(string(b))
	key := fmt.Sprintf(".AppWorkshop.WorkshopItemDetails.%v.timeupdated", modId) //  1597700547
	for _, pair := range pairs {
		if pair[0] != key {
			continue
		}

		i, err := strconv.ParseInt(pair[1], 10, 64)
		if err != nil {
			return 0, errors.Wrapf(err, "strconv.ParseInt(%v)", pair[1])
		}
		updated = int(i)
	}

	if updated == 0 {
		return 0, errors.Errorf("not found updated time of %v", modId)
	}

	return updated, nil
}

func (steamcmd *SteamCmd) createDotMod(ctx context.Context, appId, modId int, modTitle string) (err error) {
	modPath := steamcmd.modPath(appId, modId)
	modInfoPath := filepath.Join(modPath, "mod.info")
	modmetaInfoPath := filepath.Join(modPath, "modmeta.info")
	dotModPath := filepath.Join(modPath, ".mod")
	yamlPath := filepath.Join(modPath, ".yaml")

	// mod.info
	data, err := ioutil.ReadFile(modInfoPath)
	if err != nil {
		return errors.Wrapf(err, "ioutil.ReadFile(%v)", modInfoPath)
	}

	// modName
	_, nread, err := readUe4String(bytes.NewBuffer(data))
	if err != nil {
		return errors.Wrapf(err, "read modName")
	}
	data = data[nread:]

	var mapCnt uint32
	var mapNames []string

	if err := binary.Read(bytes.NewBuffer(data), binary.LittleEndian, &mapCnt); err != nil {
		return errors.Wrapf(err, "binary.Read(mapCnt)")
	}
	data = data[4:]

	for i := uint32(0); i < mapCnt; i++ {
		mapName, nread, err := readUe4String(bytes.NewBuffer(data))
		if err != nil {
			return errors.Wrap(err, "read mapName")
		}
		data = data[nread:]
		mapNames = append(mapNames, mapName)
	}

	// modmeta.info
	data, err = ioutil.ReadFile(modmetaInfoPath)
	if err != nil {
		return errors.Wrapf(err, "ioutil.ReadFile(%v)", modmetaInfoPath)
	}

	var metaCnt uint32
	var meta [][]string
	var hasModType = false

	if err := binary.Read(bytes.NewBuffer(data), binary.LittleEndian, &metaCnt); err != nil {
		return errors.Wrapf(err, "binary.Read(metaCnt)")
	}
	data = data[4:]

	for i := uint32(0); i < metaCnt; i++ {
		k, nread, err := readUe4String(bytes.NewBuffer(data))
		if err != nil {
			return errors.Wrap(err, "read meta key")
		}
		data = data[nread:]

		v, nread, err := readUe4String(bytes.NewBuffer(data))
		if err != nil {
			return errors.Wrap(err, "read meta value")
		}
		data = data[nread:]

		if k == "ModType" {
			hasModType = true
		}

		meta = append(meta, []string{k, v})
	}

	// write .mod
	w, err := os.Create(dotModPath)
	if err != nil {
		return errors.Wrapf(err, "os.Create(%v)", dotModPath)
	}
	defer w.Close()

	// modid       uint32
	// _           uint32
	var hdr = &struct {
		modId uint32
		_     uint32
	}{
		modId: uint32(modId),
	}
	if err := binary.Write(w, binary.LittleEndian, hdr); err != nil {
		return errors.Wrapf(err, "binary.Write(.mod)")
	}

	// mod name    "Super Structures."
	if err := writeUe4String(w, modTitle); err != nil {
		return errors.Wrap(err, "write ModTitle")
	}

	// mod path    "../../../ShooterGame/Content/Mods/1999447172."
	if err := writeUe4String(w, fmt.Sprintf("../../../ShooterGame/Content/Mods/%v", modId)); err != nil {
		return errors.Wrap(err, "write install dir")
	}

	// map cnt     uint32
	if err := binary.Write(w, binary.LittleEndian, mapCnt); err != nil {
		return errors.Wrap(err, "write mapCnt")
	}

	// mapNames    []string
	for _, mapName := range mapNames {
		if err := writeUe4String(w, mapName); err != nil {
			return errors.Wrap(err, "write mapName")
		}
	}

	// magic1       33ff 22ff
	// magic2       0200 0000
	magic := &struct {
		x uint32
		y uint32
	}{
		4280483635,
		2,
	}

	if err := binary.Write(w, binary.LittleEndian, magic); err != nil {
		return errors.Wrap(err, "write magic")
	}

	// HasModType   00 | 01
	if hasModType {
		if _, err := w.Write([]byte{0x01}); err != nil {
			return errors.Wrap(err, "write ModType(1)")
		}
	} else {
		if _, err := w.Write([]byte{0x00}); err != nil {
			return errors.Wrap(err, "write ModType(0)")
		}
	}

	// metaCnt      uint32
	if err := binary.Write(w, binary.LittleEndian, metaCnt); err != nil {
		return errors.Wrap(err, "write metaCnt")
	}

	// [] meta      string string
	for _, pair := range meta {
		if err := writeUe4String(w, pair[0]); err != nil {
			return errors.Wrap(err, "write meta key")
		}
		if err := writeUe4String(w, pair[1]); err != nil {
			return errors.Wrap(err, "write meta value")
		}
	}

	modUpdated, err := steamcmd.readUpdatedFromAcf(appId, modId)
	if err != nil {
		return errors.Wrap(err, "steamcmd.readUpdatedFromAcf")
	}

	// write .yaml
	if err := steamcmd.writeYaml(yamlPath, modTitle, modUpdated); err != nil {
		return errors.Wrap(err, "steamcmd.writeYaml")
	}

	return nil
}

type ModInfo struct {
	Title   string `yaml:"title"`
	Updated int    `yaml:"updated"`
}

func (steamcmd *SteamCmd) writeYaml(filepath string, title string, updated int) (err error) {
	f, err := os.Create(filepath)
	if err != nil {
		return errors.Wrapf(err, "os.Create(%v)", filepath)
	}
	defer f.Close()

	modInfo := &ModInfo{
		Title:   title,
		Updated: updated,
	}

	if err := yaml.NewEncoder(f).Encode(modInfo); err != nil {
		return errors.Wrap(err, "write yaml file")
	}

	return nil
}

func (steamcmd *SteamCmd) readYaml(filepath string) (title string, updated int, err error) {
	f, err := os.Open(filepath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", 0, err
		}
		return "", 0, errors.Wrapf(err, "os.Open(%v)", filepath)
	}
	defer f.Close()

	var modInfo = new(ModInfo)

	if err := yaml.NewDecoder(f).Decode(modInfo); err != nil {
		return "", 0, errors.Wrap(err, "read yaml file")
	}

	return modInfo.Title, modInfo.Updated, nil

}

func (steamcmd *SteamCmd) modsRoot() string {
	return filepath.Join(steamcmd.installDir, "ShooterGame", "Content", "Mods")
}

func (steamcmd *SteamCmd) installMod(ctx context.Context, appId, modId int) (err error) {
	modPath := filepath.Join(steamcmd.modsRoot(), fmt.Sprintf("%v", modId))
	modFile := filepath.Join(steamcmd.modsRoot(), fmt.Sprintf("%v.mod", modId))
	yamlFile := filepath.Join(steamcmd.modsRoot(), fmt.Sprintf("%v.yaml", modId))

	src := steamcmd.modPath(appId, modId)

	if err := os.RemoveAll(modPath); err != nil {
		if !os.IsNotExist(err) {
			return errors.Wrapf(err, "os.RemoveAll(%v)", modPath)
		}
	}

	if err := os.Remove(modFile); err != nil {
		if !os.IsNotExist(err) {
			return errors.Wrapf(err, "os.RemoveAll(%v)", modFile)
		}
	}

	if err := os.Remove(yamlFile); err != nil {
		if !os.IsNotExist(err) {
			return errors.Wrapf(err, "os.RemoveAll(%v)", yamlFile)
		}
	}

	if err := os.Rename(src, modPath); err != nil {
		return errors.Wrapf(err, "os.Rename(%v, %v)", src, modPath)
	}

	srcModFile := filepath.Join(modPath, ".mod")
	if err := os.Rename(srcModFile, modFile); err != nil {
		return errors.Wrapf(err, "os.Rename(%v, %v)", srcModFile, modFile)
	}

	srcYamlFile := filepath.Join(modPath, ".yaml")
	if err := os.Rename(srcYamlFile, yamlFile); err != nil {
		return errors.Wrapf(err, "os.Rename(%v, %v)", srcYamlFile, yamlFile)
	}

	return nil
}

func readAcf(lines []string, prefix string) (nread int, pairs [][]string) {
	var prev string

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		line = strings.TrimSpace(line)
		arr := strings.SplitN(line, "\t", 2)

		if len(arr) == 0 {
			continue
		}

		var k, v string

		if len(arr) > 0 {
			k = arr[0]
		}

		if len(arr) > 1 {
			v = arr[1]
		}

		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		k = strings.Trim(k, "\"")
		v = strings.Trim(v, "\"")

		if k == "{" {
			nread, sub := readAcf(lines[i+1:], fmt.Sprintf("%v.%v", prefix, prev))
			i = i + nread
			pairs = append(pairs, sub...)
		} else if k == "}" {
			return i + 1, pairs
		} else if v == "" {
			prev = k
		} else {
			pairs = append(pairs, []string{fmt.Sprintf("%v.%v", prefix, k), v})
		}
	}

	return len(lines), pairs
}

func ReadAcf(input string) (pairs [][]string) {
	_, pairs = readAcf(strings.Split(input, "\n"), "")
	return pairs
}
