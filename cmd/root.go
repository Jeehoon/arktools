/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/jeehoon/arktools/pkg/log"
)

var cfgFile string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "arktools",
	Short: "ARK Server Management Tools",
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)
	cobra.OnInitialize(setDefaults)

	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.arktools.yaml)")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "verbose logging")

	rootCmd.PersistentFlags().String("install-dir", "", "ARK server install dir (default is $HOME/ARK)")
	rootCmd.PersistentFlags().String("steamcmd", "", "SteamCMD location (default is $HOME/steamcmd/steamcmd.sh)")
	rootCmd.PersistentFlags().BoolP("check", "c", false, "check mode")
	rootCmd.PersistentFlags().BoolP("force", "f", false, "force mode")
	rootCmd.PersistentFlags().String("output", "stdout", "output execution result")

	cobra.CheckErr(viper.BindPFlags(rootCmd.PersistentFlags()))
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		// Search config in home directory with name ".arktools" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".arktools")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}

	if viper.GetBool("verbose") {
		log.Verbose = true
	} else {
		log.Verbose = false
	}

}

var Output io.Writer

func setDefaults() {
	home, err := os.UserHomeDir()
	cobra.CheckErr(err)

	if viper.GetString("install-dir") == "" {
		viper.Set("install-dir", filepath.Join(home, "ARK"))
	}

	if viper.GetString("steamcmd") == "" {
		viper.Set("steamcmd", filepath.Join(home, "steamcmd", "steamcmd.sh"))
	}

	if output := viper.GetString("output"); output == "stdout" {
		Output = os.Stdout
	} else {
		f, err := os.Create(output)
		cobra.CheckErr(err)
		Output = f
	}

}
