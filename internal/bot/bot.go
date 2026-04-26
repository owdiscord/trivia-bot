// Package bot contains our central Bot type, which holds our database,
// active question, the configuration, and discordgo session.
package bot

import (
	"fmt"
	"log/slog"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/owdiscord/dcc/internal/db"
)

var accent = 0xEA6012
var buttonEmojis = []discordgo.ComponentEmoji{
	{Name: "🟩"},
	{Name: "🔶"},
	{Name: "🔵"},
	{Name: "💜"},
	{Name: "♦️"},
}

type Answer struct {
	Text    string
	Correct bool
}

type Round struct {
	ID       string
	Channel  string
	EndsAt   time.Time
	Question db.Trivia
	Shuffled []Answer

	Responses map[string]int
	Stat      *db.RoundStat
	Promoted  []string

	closed bool
}

type Bot struct {
	session *discordgo.Session
	store   *db.PointStore
	stats   *db.StatStore

	schedule time.Duration
	timeout  time.Duration

	guildID   string
	addRoleID string
	channels  []string
	trivia    []*db.Trivia

	mu     sync.Mutex
	active map[string]*Round
}

func New(session *discordgo.Session, guildID string, addRoleID string, store *db.PointStore, stats *db.StatStore, schedule time.Duration, timeout time.Duration, channels []string, trivia []*db.Trivia) *Bot {
	return &Bot{
		session:   session,
		store:     store,
		stats:     stats,
		schedule:  schedule,
		timeout:   timeout,
		guildID:   guildID,
		addRoleID: addRoleID,
		channels:  channels,
		trivia:    trivia,
		active:    map[string]*Round{},
	}
}

// Scheduler

func (b *Bot) StartScheduler() {
	slog.Info("Starting scheduler")
	b.SendQuestion()

	ticker := time.NewTicker(b.schedule)

	go func() {
		for range ticker.C {
			b.SendQuestion()
		}
	}()
}

// Question Flow

func (b *Bot) SendQuestion() {
	b.mu.Lock()
	defer b.mu.Unlock()

	question := b.trivia[rand.Intn(len(b.trivia))]
	channel := b.channels[rand.Intn(len(b.channels))]

	if _, exists := b.active[channel]; exists {
		return
	}

	var answers []Answer
	for answer, isCorrect := range question.Answers {
		answers = append(answers, Answer{
			Text:    answer,
			Correct: isCorrect,
		})
	}

	correctAnswer, answerPool := buildFairOptions(answers)
	emojis := randomisedEmojis()

	slog.Info("question being sent", "question", question, "answers", answers)

	embed := buildInitialEmbed(question, answerPool, emojis)

	msg, err := b.session.ChannelMessageSendComplex(channel, &discordgo.MessageSend{
		Flags:      discordgo.MessageFlagsIsComponentsV2,
		Components: embed,
	})
	if err != nil {
		slog.Error("failed to send question", "err", err)
		return
	}

	// Text of the answer options to be used for stat storage
	options := make([]string, len(answerPool))
	for i, a := range answerPool {
		options[i] = a.Text
	}

	round := &Round{
		ID:        msg.ID,
		Channel:   channel,
		EndsAt:    time.Now().Add(b.timeout),
		Question:  *question,
		Shuffled:  answerPool,
		Responses: map[string]int{},
		Stat: b.stats.RecordRoundOpened(
			msg.ID,
			channel,
			question.Question,
			options,
			correctAnswer,
			time.Now(),
		),
	}

	b.active[channel] = round

	go b.closeRound(round)
}

// Interaction Handling

func (b *Bot) HandleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionMessageComponent {
		return
	}

	channelID := i.ChannelID
	userID := i.Member.User.ID

	b.mu.Lock()
	round, ok := b.active[channelID]
	b.mu.Unlock()

	if !ok || round == nil {
		return
	}

	if time.Now().After(round.EndsAt) || round.closed {
		return
	}

	if _, exists := round.Responses[userID]; exists {
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "You've already submitted an answer!",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	idx, err := strconv.Atoi(i.MessageComponentData().CustomID)
	if err != nil {
		return
	}

	slog.Debug("adding response for user", "user_id", userID, "interaction_id", i.ID)
	round.Responses[userID] = idx
	b.stats.RecordVote(round.Stat, userID, idx)

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("You answered: **%s** — good luck!", round.Shuffled[idx].Text),
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

// Round Closing

func (b *Bot) closeRound(round *Round) {
	timer := time.NewTimer(time.Until(round.EndsAt))
	defer timer.Stop()

	<-timer.C

	b.mu.Lock()

	current, ok := b.active[round.Channel]
	if !ok || current != round {
		b.mu.Unlock()
		return
	}

	if round.closed {
		b.mu.Unlock()
		return
	}

	slog.Info("closing round", "round", round)

	round.closed = true
	delete(b.active, round.Channel)

	b.mu.Unlock()
	b.stats.RecordRoundClosed(round.Stat)

	winners := []string{}

	for userID, idx := range round.Responses {
		// Jetpack Cat Clause
		meowMeowMeow := strings.Contains(strings.ToLower(round.Question.Question), "jetpack cat") && !strings.Contains(round.Shuffled[idx].Text, "Woof")

		if round.Shuffled[idx].Correct || meowMeowMeow {
			newPoints := b.store.Add(userID, 1)

			// Promote the user to the new role when they hit 7 points
			if newPoints == 7 {
				round.Promoted = append(round.Promoted, userID)
			}

			if len(winners) < 12 {
				winners = append(winners, userID)
			}
		}
	}

	updatedEmbed := buildTimesUpEmbed(round, winners)

	_, err := b.session.ChannelMessageEditComplex(&discordgo.MessageEdit{
		ID:         round.ID,
		Channel:    round.Channel,
		Flags:      discordgo.MessageFlagsIsComponentsV2,
		Components: &updatedEmbed,
	})
	if err != nil {
		slog.Error("failed to edit message", "msg_id", round.ID, "channel_id", round.Channel, "err", err)
	}

	for _, userID := range round.Promoted {
		slog.Info("adding role to user", "guild_id", b.guildID, "user_id", userID, "role_id", b.addRoleID)
		if err := b.session.GuildMemberRoleAdd(b.guildID, userID, b.addRoleID); err != nil {
			slog.Error("failed to apply reward role", "user_id", userID, "err", err)
		}
	}
}

// Utils
func buildFairOptions(all []Answer) (string, []Answer) {
	var correct Answer
	var wrong []Answer

	for _, a := range all {
		if a.Correct {
			correct = a
		} else {
			wrong = append(wrong, a)
		}
	}

	// shuffle wrong pool first
	rand.Shuffle(len(wrong), func(i, j int) {
		wrong[i], wrong[j] = wrong[j], wrong[i]
	})

	// pick up to 4 wrong answers
	n := min(len(wrong), 4)

	selected := append([]Answer{correct}, wrong[:n]...)

	// if not enough total answers, pad safely (fallback)
	for len(selected) < 5 && len(wrong) > n {
		selected = append(selected, wrong[n])
		n++
	}

	// final shuffle so correct isn't always first
	rand.Shuffle(len(selected), func(i, j int) {
		selected[i], selected[j] = selected[j], selected[i]
	})

	return correct.Text, selected
}

// Shuffle emoji order. Doesn't really need
// to be properly randomised or do much work here.
func randomisedEmojis() []discordgo.ComponentEmoji {
	emojis := make([]discordgo.ComponentEmoji, len(buttonEmojis))
	copy(emojis, buttonEmojis)
	rand.Shuffle(len(emojis), func(i, j int) {
		emojis[i], emojis[j] = emojis[j], emojis[i]
	})

	return emojis
}

func buildVotingButtons(answerPool []Answer, emojis []discordgo.ComponentEmoji) []discordgo.MessageComponent {
	components := []discordgo.MessageComponent{}
	for i := range answerPool {
		components = append(components, discordgo.Button{
			Emoji:    &emojis[i],
			Label:    "",
			Style:    discordgo.SecondaryButton,
			CustomID: fmt.Sprintf("%d", i),
		})
	}

	return components
}

// Create the initial trivia component
func buildInitialEmbed(question *db.Trivia, answerPool []Answer, emojis []discordgo.ComponentEmoji) []discordgo.MessageComponent {
	var embedText strings.Builder
	embedText.WriteString("## Trivia time!\n" + question.Question + "\n\n")
	for i, answer := range answerPool {
		embedText.WriteString(emojis[i].Name + "  " + answer.Text + "\n")
	}

	buttons := buildVotingButtons(answerPool, emojis)

	return []discordgo.MessageComponent{
		discordgo.Container{
			Spoiler:     false,
			AccentColor: &accent,
			Components: []discordgo.MessageComponent{
				discordgo.TextDisplay{
					Content: embedText.String(),
				},
				discordgo.ActionsRow{
					Components: buttons,
				},
			},
		},
	}
}

func buildTimesUpEmbed(round *Round, winners []string) []discordgo.MessageComponent {
	correctAnswer := func() string {
		for answer, correct := range round.Question.Answers {
			if correct {
				return answer
			}
		}

		return ""
	}()

	winnerString := func() string {
		var b strings.Builder
		first := true
		for i, winner := range winners {
			// Only keep up to 20 winners in the list
			if i > 20 {
				break
			}

			if !first {
				b.WriteString(", ")
			}
			b.WriteString("<@" + winner + ">")
			first = false
		}

		return b.String()
	}()

	if winnerString == "" {
		winnerString = "Nobody! Wow, nobody got the question right this time!"
	}

	return []discordgo.MessageComponent{
		discordgo.Container{
			AccentColor: &accent,
			Spoiler:     false,
			Components: []discordgo.MessageComponent{
				discordgo.TextDisplay{
					Content: "## Time's up! \n\nThe correct answer to '" + round.Question.Question + "' is **" + correctAnswer + "**\n\n**Winners: **" + winnerString,
				},
			},
		},
	}
}

func applyRoles() {

}
