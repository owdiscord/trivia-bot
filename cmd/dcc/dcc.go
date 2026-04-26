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
	"github.com/owdiscord/dcc/internal/commands"
	"github.com/owdiscord/dcc/internal/db"
)

func main() {
	token := os.Getenv("BOT_TOKEN")
	if len(token) < 10 {
		slog.Error("could not start bot", "env_err", "BOT_TOKEN was not set")
		return
	}

	guildID := os.Getenv("GUILD_ID")
	if len(guildID) < 10 {
		slog.Error("could not start bot", "env_err", "GUILD_ID was not set")
		return
	}

	joinedChannels := os.Getenv("CHANNEL_IDS")
	if len(joinedChannels) < 10 {
		slog.Error("could not start bot", "env_err", "CHANNEL_IDS was not set")
		return
	}

	addRoleID := os.Getenv("SPECIAL_ROLE_ID")
	if len(addRoleID) < 10 {
		slog.Error("could not start bot", "env_err", "SPECIAL_ROLE_ID was not set")
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

	stats, err := db.NewStatStore("test_stats.json")
	if err != nil {
		slog.Error("could not init stats store", "err", err)
		return
	}

	b := bot.New(dg, guildID, addRoleID, store, stats, time.Minute*10, time.Second*20, channels, trivia)

	dg.AddHandler(b.HandleInteraction)

	if err = dg.Open(); err != nil {
		slog.Error("could not open discord gateway", "discord_err", err)
		return
	}

	registeredCmds := make([]*discordgo.ApplicationCommand, len(commands.Commands))
	for i, cmd := range commands.Commands {
		registered, err := dg.ApplicationCommandCreate(dg.State.User.ID, guildID, cmd)
		if err != nil {
			slog.Error("could not register command", "cmd", cmd.Name, "err", err)
			return
		}
		registeredCmds[i] = registered
		slog.Info("registered command", "cmd", cmd.Name)
	}

	// Wire command handler
	dg.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if i.Type != discordgo.InteractionApplicationCommand {
			return
		}
		if i.ApplicationCommandData().Name == "trivia" {
			commands.HandleTrivia(s, i, store)
		}
	})

	b.StartScheduler()

	fmt.Println("Bot is now running. Press CTRL-C to exit.")

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	// Clean up commands on exit
	for _, cmd := range registeredCmds {
		if err := dg.ApplicationCommandDelete(dg.State.User.ID, guildID, cmd.ID); err != nil {
			slog.Warn("could not delete command", "cmd", cmd.Name, "err", err)
		}
	}

	_ = dg.Close()
}
