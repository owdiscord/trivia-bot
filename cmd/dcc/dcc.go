package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/owdiscord/dcc/internal/bot"
	"github.com/owdiscord/dcc/internal/db"
)

func main() {
	token := os.Getenv("BOT_TOKEN")
	if len(token) < 10 {
		slog.Error("could not start bot", "env_err", "BOT_TOKEN was not set")
		return
	}

	joinedChannels := os.Getenv("CHANNEL_IDS")
	if len(joinedChannels) < 10 {
		slog.Error("could not start bot", "env_err", "CHANNEL_IDS was not set")
		return
	}

	channels := strings.Split(joinedChannels, ",")

	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		slog.Error("could not create discordgo instance", "discord_err", err)
		return
	}

	trivia, err := db.ReadTrivia("test.csv")
	if err != nil {
		slog.Error("could not read trivia config", "trivia_err", err)
		return
	}

	store, err := db.NewPointStore("test_points.json")
	if err != nil {
		slog.Error("could not init store", "err", err)
		return
	}

	b := bot.New(dg, store, time.Minute*10, time.Second*20, channels, trivia)

	dg.AddHandler(b.HandleInteraction)

	if err = dg.Open(); err != nil {
		slog.Error("could not open discord gateway", "discord_err", err)
		return
	}

	b.StartScheduler()

	fmt.Println("Bot is now running. Press CTRL-C to exit.")

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	_ = dg.Close()
}
