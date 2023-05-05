/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"strconv"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/jeehoon/arktools/pkg/steamcmd"
)

// updatemodCmd represents the updatemod command
var updatemodCmd = &cobra.Command{
	Use:   "updatemod",
	Short: "Update ARK Server",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()

		var modIds []int
		for _, arg := range args {
			i, err := strconv.ParseInt(arg, 10, 64)
			cobra.CheckErr(err)

			modIds = append(modIds, int(i))
		}

		cobra.CheckErr(doUpdateMods(ctx, modIds))
	},
}

func init() {
	cobra.OnInitialize(setDefaults)

	rootCmd.AddCommand(updatemodCmd)

	// for mod updatemod
	updatemodCmd.Flags().Int("mod-appid", 346110, "ARK Mod AppId")
	updatemodCmd.Flags().IntSlice("modids", nil, "modid list, comma separated string")

	cobra.CheckErr(viper.BindPFlags(updatemodCmd.Flags()))
}

func doUpdateMods(ctx context.Context, modIds []int) (err error) {

	installDir := viper.GetString("install-dir")
	steamcmdExec := viper.GetString("steamcmd")
	modAppId := viper.GetInt("mod-appid")
	check := viper.GetBool("check")
	force := viper.GetBool("force")

	scmd := steamcmd.NewSteamCmd(steamcmdExec, installDir)
	scmd.SetOutput(Output)

	// mods
	var updatedModids []int
	if force {
		updatedModids = modIds
	} else {
		updatedModids, err = scmd.UpdateRequiredMods(ctx, modAppId, modIds)
		if err != nil {
			return errors.Wrapf(err, "scmd.UpdateRequiredMods(%v)", modAppId)
		}
	}

	if len(updatedModids) > 0 && !check {
		if err := scmd.UpdateMods(ctx, modAppId, updatedModids); err != nil {
			return errors.Wrapf(err, "steamcmd.UpdateMods(%v, %v)", modAppId, updatedModids)
		}
	}

	return nil
}
