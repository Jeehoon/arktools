/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/jeehoon/arktools/pkg/steamcmd"
)

// updateCmd represents the update command
var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update ARK Server",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()
		cobra.CheckErr(doUpdate(ctx))
	},
}

func init() {
	cobra.OnInitialize(setDefaults)

	rootCmd.AddCommand(updateCmd)

	updateCmd.Flags().Int("appid", 376030, "ARK AppId")

	cobra.CheckErr(viper.BindPFlags(updateCmd.Flags()))
}

func doUpdate(ctx context.Context) (err error) {

	installDir := viper.GetString("install-dir")
	steamcmdExec := viper.GetString("steamcmd")
	appId := viper.GetInt("appid")
	check := viper.GetBool("check")
	force := viper.GetBool("force")

	scmd := steamcmd.NewSteamCmd(steamcmdExec, installDir)
	scmd.SetOutput(Output)

	// server
	var hasUpdate bool
	if force {
		hasUpdate = true
	} else {
		hasUpdate, err = scmd.HasUpdate(ctx, appId)
		if err != nil {
			return errors.Wrapf(err, "scmd.HasUpdate(%v)", appId)
		}
	}

	if hasUpdate && !check {
		if err := scmd.UpdateServer(ctx, appId); err != nil {
			return errors.Wrapf(err, "steamcmd.UpdateServer(%v)", appId)
		}
	}

	return nil
}
