package cmd

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// fcssCmd represents the fcss command
var fcssCmd = &cobra.Command{
	Use:   "fcss",
	Short: "FCSS ARK Server",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()
		cobra.CheckErr(doFCSS(ctx, args))
	},
}

func init() {
	cobra.OnInitialize(setDefaults)

	rootCmd.AddCommand(fcssCmd)

	fcssCmd.Flags().String("fcss-dir", "gamedata/Clusters/FCSS", "FCSS install dir")

	cobra.CheckErr(viper.BindPFlags(fcssCmd.Flags()))
}

func doFCSS(ctx context.Context, args []string) error {

	matches, err := filepath.Glob(filepath.Join(viper.GetString("fcss-dir"), "Players", "*.sav"))
	if err != nil {
		return errors.Wrap(err, "filapath.Glob")
	}

	var players []any

	for _, path := range matches {
		out, err := ReadSav(path)
		if err != nil {
			return errors.Wrap(err, "ReadSav")
		}

		var player any
		if err := json.Unmarshal(out, &player); err != nil {
			return errors.Wrap(err, "json.Unmarshal")
		}
		players = append(players, player)
	}

	if err := json.NewEncoder(os.Stdout).Encode(players); err != nil {
		return errors.Wrap(err, "json.Encode")
	}

	return nil
}

func readString(data []byte) (s string, nread int) {
	l := binary.LittleEndian.Uint32(data)
	data = data[4:]

	s = string(data[:l])
	data = data[l:]

	return s, int(l) + 4
}

func ToUtf16le(utf8 []byte) (utf16le []byte, err error) {
	utf16le, _, err = transform.Bytes(unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM).NewEncoder(), utf8)
	return
}

func ToUtf8(utf16le []byte) (utf8 []byte, err error) {
	utf8, _, err = transform.Bytes(unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM).NewDecoder(), utf16le)
	return
}

func ReadSav(filepath string) (out []byte, err error) {
	data, err := ioutil.ReadFile(filepath)
	if err != nil {
		return nil, errors.Wrap(err, "ioutil.ReadFile")
	}

	var nread int

	_, nread = readString(data)
	data = data[nread:]

	_, nread = readString(data)
	data = data[nread:]

	_, nread = readString(data)
	data = data[nread:]

	length := binary.LittleEndian.Uint64(data)
	data = data[8:]

	code := data[0:4]
	data = data[4:]

	json := data[:length-4]
	data = data[length-4:]

	if code[3] == 255 {
		utf8, err := ToUtf8(json)
		if err != nil {
			return nil, errors.Wrap(err, "ToUtf8")
		}
		out = utf8
	} else {
		out = json
	}

	out = bytes.Trim(out, "\x00")
	return out, nil
}
