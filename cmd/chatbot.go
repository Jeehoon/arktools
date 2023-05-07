/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/jeehoon/arktools/pkg/chatbot"
)

// chatbotCmd represents the chatbot command
var chatbotCmd = &cobra.Command{
	Use:   "chatbot",
	Short: "ChatBot Agent",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()
		cobra.CheckErr(doChatBot(ctx, args))
	},
}

func init() {
	cobra.OnInitialize(setDefaults)

	rootCmd.AddCommand(chatbotCmd)

	chatbotCmd.Flags().String("api-token", "", "discord api token")
	chatbotCmd.Flags().String("webdis-addr", "http://127.0.0.1:7379", "webdus address")
	chatbotCmd.Flags().String("chat-cluster", "MyCluster", "chat cluster id")
	chatbotCmd.Flags().String("channel-id", "MyCluster", "discord channel id")
	chatbotCmd.Flags().String("chat-fmt", "```md\n[%v][%v][%v]: %v\n```", "discord display format")

	cobra.CheckErr(viper.BindPFlags(chatbotCmd.Flags()))
}

func doChatBot(ctx context.Context, args []string) (err error) {
	apiToken := viper.GetString("api-token")
	webdisAddr := viper.GetString("webdis-addr")
	chatCluster := viper.GetString("chat-cluster")
	channelId := viper.GetString("channel-id")
	chatFmt := viper.GetString("chat-fmt")

	cb := chatbot.New(apiToken, webdisAddr, chatCluster, channelId, chatFmt)

	if err := cb.Serve(ctx); err != nil {
		return errors.Wrap(err, "Serve")
	}
	return nil
}
