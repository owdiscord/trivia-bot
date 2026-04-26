// Package commands contains our Discord slash commands
package commands

import (
	"fmt"
	"slices"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/owdiscord/dcc/internal/db"
)

const staffRoleID = "968480104483291196"

func ptr[T any](v T) *T { return &v }

var Commands = []*discordgo.ApplicationCommand{
	{
		Name:                     "trivia",
		Description:              "Parent command for all trivia-related commands",
		DefaultMemberPermissions: ptr(int64(0)), // hidden from everyone by default
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "leaderboard",
				Description: "View the top trivia players",
			},
		},
	},
}

// HasStaffRole checks whether the interaction member has the staff role.
func HasStaffRole(i *discordgo.InteractionCreate) bool {
	if i.Member == nil {
		return false
	}

	// Bypass the role check if we are a server admin
	if i.Member.Permissions&discordgo.PermissionManageGuild != 0 {
		return true
	}

	return slices.Contains(i.Member.Roles, staffRoleID)
}

func HandleTrivia(s *discordgo.Session, i *discordgo.InteractionCreate, store *db.PointStore) {
	if !HasStaffRole(i) {
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "You don't have permission to use this command.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		return
	}

	switch options[0].Name {
	case "leaderboard":
		handleLeaderboard(s, i, store)
	}
}

func handleLeaderboard(s *discordgo.Session, i *discordgo.InteractionCreate, store *db.PointStore) {
	top := store.TopN(10)

	var sb strings.Builder
	sb.WriteString("## 🏆 Trivia Leaderboard\n\n")

	if len(top) == 0 {
		sb.WriteString("No scores yet — get quizzing!")
	} else {
		medals := []string{"🥇", "🥈", "🥉"}
		for idx, entry := range top {
			prefix := fmt.Sprintf("%d.", idx+1)
			if idx < len(medals) {
				prefix = medals[idx]
			}
			fmt.Fprintf(&sb, "%s <@%s> — **%d points**\n", prefix, entry.UserID, entry.Points)
		}
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsIsComponentsV2,
			Components: []discordgo.MessageComponent{
				discordgo.Container{
					Components: []discordgo.MessageComponent{
						discordgo.TextDisplay{Content: sb.String()},
					},
				},
			},
		},
	})
}
