package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	_ "modernc.org/sqlite"
)

const (
	dbPath            = "roachbot.db"
	maxRoachAge       = 30
	fullCooldown      = 4 * time.Hour
	fullEverySuccess  = 3
	defaultRoachEmote = "🪳"
	embedColor        = 0x7A5C38

	moodGreat   = "great"
	moodNeutral = "neutral"
	moodBad     = "bad"
)

type roach struct {
	ID        int64
	Name      string
	OwnerID   string
	CreatedAt time.Time
	DiedAt    sql.NullTime
	Age       int
	Health    int
	Mood      string
	FullUntil sql.NullTime
}

type bot struct {
	db  *sql.DB
	rng *rand.Rand
}

func main() {
	loadDotEnv(".env")

	token := strings.TrimSpace(os.Getenv("DISCORD_TOKEN"))
	if token == "" {
		log.Fatal("DISCORD_TOKEN is required")
	}

	db, err := openDB(dbPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	session, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Fatalf("create discord session: %v", err)
	}

	app := &bot{
		db:  db,
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	session.AddHandler(app.onInteractionCreate)
	session.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		log.Printf("logged in as %s", r.User.String())
	})

	if err := session.Open(); err != nil {
		log.Fatalf("open discord session: %v", err)
	}
	defer session.Close()

	appID := strings.TrimSpace(os.Getenv("DISCORD_APP_ID"))
	if appID == "" && session.State.User != nil {
		appID = session.State.User.ID
	}
	guildID := strings.TrimSpace(os.Getenv("DISCORD_GUILD_ID"))

	if err := registerCommands(session, appID, guildID); err != nil {
		log.Fatalf("register commands: %v", err)
	}

	log.Println("roachbot is skittering. press Ctrl+C to stop.")
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Println("roachbot tucked itself under the fridge. goodbye.")
}

func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)

	schema := `
CREATE TABLE IF NOT EXISTS roach_master (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	roach_name TEXT NOT NULL,
	owner_id TEXT NOT NULL,
	created_at DATETIME NOT NULL,
	died_at DATETIME,
	roach_age INTEGER NOT NULL DEFAULT 0,
	roach_health INTEGER NOT NULL DEFAULT 10,
	roach_mood TEXT NOT NULL DEFAULT 'neutral',
	roach_full_until DATETIME
);

CREATE INDEX IF NOT EXISTS idx_roach_master_owner_living
ON roach_master(owner_id, died_at);
`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureColumn(db, "roach_master", "roach_full_until", "DATETIME"); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

func ensureColumn(db *sql.DB, table, column, definition string) error {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull int
		var defaultValue sql.NullString
		var primaryKey int
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	_, err = db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
	return err
}

func registerCommands(s *discordgo.Session, appID, guildID string) error {
	commands := []*discordgo.ApplicationCommand{
		{
			Name:        "catch-a-roach",
			Description: "Catch a tiny new friend from the cozy corners.",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "name",
					Description: "Your roach's name.",
					Required:    true,
					MaxLength:   40,
				},
			},
		},
		{Name: "feed-the-roach", Description: "Offer snacks and hope your roach accepts the cuisine."},
		{Name: "pet-the-roach", Description: "Give your roach careful, tiny pats."},
		{Name: "check-roach-mood", Description: "See how your roach is feeling."},
		{Name: "check-roach-profile", Description: "Inspect your living roach's profile."},
		{Name: "check-past-roaches", Description: "Remember roaches who have crossed the baseboard."},
	}

	for _, command := range commands {
		if _, err := s.ApplicationCommandCreate(appID, guildID, command); err != nil {
			return fmt.Errorf("create /%s: %w", command.Name, err)
		}
	}
	return nil
}

func (b *bot) onInteractionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	userID := interactionUserID(i)
	userName := interactionUserName(i)
	data := i.ApplicationCommandData()

	var response string
	var err error

	switch data.Name {
	case "catch-a-roach":
		name := optionString(data.Options, "name")
		response, err = b.catchRoach(ctx, userID, name)
	case "feed-the-roach":
		response, err = b.feedRoach(ctx, userID)
	case "pet-the-roach":
		response, err = b.petRoach(ctx, userID)
	case "check-roach-mood":
		response, err = b.checkMood(ctx, userID)
	case "check-roach-profile":
		response, err = b.checkProfile(ctx, userID, userName)
	case "check-past-roaches":
		response, err = b.checkPastRoaches(ctx, userID)
	default:
		response = "That command fell behind the cabinet."
	}

	if err != nil {
		log.Printf("command /%s failed for %s: %v", data.Name, userID, err)
		response = "The roach bureaucracy jammed. Try again in a moment."
	}

	if err := respond(s, i, commandTitle(data.Name), response); err != nil {
		log.Printf("respond: %v", err)
	}
}

func (b *bot) catchRoach(ctx context.Context, ownerID, name string) (string, error) {
	name = cleanRoachName(name)
	if name == "" {
		return "That name vanished into the wall. Try a name with letters or numbers.", nil
	}

	living, err := b.livingRoach(ctx, ownerID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if living != nil {
		return fmt.Sprintf("You already have %s, who is very busy polishing one antenna. One living roach at a time.", bold(living.Name)), nil
	}

	now := time.Now().UTC()
	health := 10
	mood := moodNeutral
	res, err := b.db.ExecContext(ctx, `
INSERT INTO roach_master (roach_name, owner_id, created_at, roach_age, roach_health, roach_mood)
VALUES (?, ?, ?, 0, ?, ?)
`, name, ownerID, now, health, mood)
	if err != nil {
		return "", err
	}
	id, _ := res.LastInsertId()

	return fmt.Sprintf("You lifted a snack wrapper and found %s! Roach #%d has joined your household with %d/10 health and a neutral little heart.", bold(name), id, health), nil
}

func (b *bot) feedRoach(ctx context.Context, ownerID string) (string, error) {
	r, err := b.livingRoach(ctx, ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		return "You do not have a living roach yet. Use `/catch-a-roach` and start the tiny legacy.", nil
	}
	if err != nil {
		return "", err
	}

	now := time.Now().UTC()
	if r.FullUntil.Valid && now.Before(r.FullUntil.Time) {
		return fmt.Sprintf("%s is still full and politely shoves the snack back under the napkin. Try again later.", bold(r.Name)), nil
	}

	chance := feedChance(r)
	roll := b.rng.Intn(10) + 1
	if roll <= chance {
		r.Age++
		r.Health = min(10, r.Health+1)
		if r.Mood == moodBad {
			r.Mood = moodNeutral
		}

		if r.Age >= maxRoachAge {
			if err := b.updateRoach(ctx, r.ID, r.Age, r.Health, r.Mood, &now, nil); err != nil {
				return "", err
			}
			return fmt.Sprintf("%s enjoyed a perfect crumb banquet and reached age %d/%d. After a glorious life of snacks, naps, and dramatic antenna poses, they peacefully retired to the Great Pantry.", bold(r.Name), r.Age, maxRoachAge), nil
		}

		var fullUntil *time.Time
		if r.Age%fullEverySuccess == 0 {
			nextFeed := now.Add(fullCooldown)
			fullUntil = &nextFeed
		}

		if err := b.updateRoach(ctx, r.ID, r.Age, r.Health, r.Mood, nil, fullUntil); err != nil {
			return "", err
		}
		if fullUntil != nil {
			return fmt.Sprintf("%s accepted the offering and is now deeply, magnificently full.\nHealth: %d/10\nAge: %d/%d successful feeds", bold(r.Name), r.Health, r.Age, maxRoachAge), nil
		}
		return fmt.Sprintf("%s accepted the offering. Tiny crunches. Big feelings.\nHealth: %d/10\nAge: %d/%d successful feeds", bold(r.Name), r.Health, r.Age, maxRoachAge), nil
	}

	r.Health--
	if r.Health <= 0 {
		r.Mood = moodBad
		if err := b.updateRoach(ctx, r.ID, r.Age, 0, r.Mood, &now, nil); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s refused dinner, sighed at the cuisine, and finally gave up on this mortal kitchen. RIP, small crumb connoisseur.", bold(r.Name)), nil
	}

	if r.Mood == moodGreat {
		r.Mood = moodNeutral
	} else {
		r.Mood = moodBad
	}

	if err := b.updateRoach(ctx, r.ID, r.Age, r.Health, r.Mood, nil, nil); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s rejected the meal with theatrical legwork.\nHealth: %d/10", bold(r.Name), r.Health), nil
}

func (b *bot) petRoach(ctx context.Context, ownerID string) (string, error) {
	r, err := b.livingRoach(ctx, ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		return "No roach to pet yet. `/catch-a-roach` first, then prepare the tiny affection.", nil
	}
	if err != nil {
		return "", err
	}

	before := r.Mood
	switch r.Mood {
	case moodBad:
		r.Mood = moodNeutral
	case moodNeutral:
		r.Mood = moodGreat
	default:
		r.Health = min(10, r.Health+1)
	}

	if err := b.updateRoach(ctx, r.ID, r.Age, r.Health, r.Mood, nil, nullableTimePtr(r.FullUntil)); err != nil {
		return "", err
	}

	if before == moodGreat {
		return fmt.Sprintf("%s was already feeling great, so the gentle pats became a spa day. Health: %d/10.", bold(r.Name), r.Health), nil
	}
	return fmt.Sprintf("You pet %s with one respectful fingertip. Their tiny heart brightens.\nMood: %s", bold(r.Name), moodDescription(r.Mood)), nil
}

func (b *bot) checkMood(ctx context.Context, ownerID string) (string, error) {
	r, err := b.livingRoach(ctx, ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		return "No living roach is reporting a mood. The walls are quiet.", nil
	}
	if err != nil {
		return "", err
	}

	switch r.Mood {
	case moodGreat:
		return fmt.Sprintf("%s is feeling great. Antennae: sparkling. Confidence: unreasonable.", bold(r.Name)), nil
	case moodBad:
		return fmt.Sprintf("%s is feeling bad. They are staring at a crumb like it owes them money.", bold(r.Name)), nil
	default:
		return fmt.Sprintf("%s is neutral. A classic under-fridge philosopher mood.", bold(r.Name)), nil
	}
}

func (b *bot) checkProfile(ctx context.Context, ownerID, ownerName string) (string, error) {
	r, err := b.livingRoach(ctx, ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		return "You do not have a living roach profile yet. `/catch-a-roach` can fix that with style.", nil
	}
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(`%s
Owner: %s
Created: %s
Age: %d/%d successful feeds
Health: %d/10
Mood: %s`,
		bold(r.Name),
		ownerName,
		r.CreatedAt.Format(time.RFC822),
		r.Age,
		maxRoachAge,
		r.Health,
		moodDescription(r.Mood),
	), nil
}

func (b *bot) checkPastRoaches(ctx context.Context, ownerID string) (string, error) {
	rows, err := b.db.QueryContext(ctx, `
SELECT id, roach_name, created_at, died_at, roach_age, roach_health, roach_mood
FROM roach_master
WHERE owner_id = ? AND died_at IS NOT NULL
ORDER BY died_at DESC, id DESC
LIMIT 10
`, ownerID)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	lines := []string{"Past roaches, remembered fondly:"}
	for rows.Next() {
		var r roach
		if err := rows.Scan(&r.ID, &r.Name, &r.CreatedAt, &r.DiedAt, &r.Age, &r.Health, &r.Mood); err != nil {
			return "", err
		}
		lines = append(lines, fmt.Sprintf("#%d %s - age %d/%d, died %s", r.ID, r.Name, r.Age, maxRoachAge, r.DiedAt.Time.Format("2006-01-02")))
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(lines) == 1 {
		return "No past roaches yet. Your tiny dynasty has not become history.", nil
	}
	return strings.Join(lines, "\n"), nil
}

func (b *bot) livingRoach(ctx context.Context, ownerID string) (*roach, error) {
	var r roach
	err := b.db.QueryRowContext(ctx, `
SELECT id, roach_name, owner_id, created_at, died_at, roach_age, roach_health, roach_mood, roach_full_until
FROM roach_master
WHERE owner_id = ? AND died_at IS NULL
ORDER BY id DESC
LIMIT 1
`, ownerID).Scan(&r.ID, &r.Name, &r.OwnerID, &r.CreatedAt, &r.DiedAt, &r.Age, &r.Health, &r.Mood, &r.FullUntil)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (b *bot) updateRoach(ctx context.Context, id int64, age, health int, mood string, diedAt, fullUntil *time.Time) error {
	_, err := b.db.ExecContext(ctx, `
UPDATE roach_master
SET roach_age = ?, roach_health = ?, roach_mood = ?, died_at = ?, roach_full_until = ?
WHERE id = ?
`, age, health, mood, diedAt, fullUntil, id)
	return err
}

func feedChance(r *roach) int {
	if r.Mood == moodGreat {
		return 8
	}

	penalty := r.Age / 5
	chance := 8 - penalty
	return max(1, chance)
}

func moodDescription(mood string) string {
	switch mood {
	case moodGreat:
		return "Antennae sparkling, confidence unreasonable."
	case moodBad:
		return "Staring at a crumb like it owes them money."
	default:
		return "A classic under-fridge philosopher mood."
	}
}

func interactionUserID(i *discordgo.InteractionCreate) string {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.ID
	}
	if i.User != nil {
		return i.User.ID
	}
	return ""
}

func interactionUserName(i *discordgo.InteractionCreate) string {
	if i.Member != nil {
		if i.Member.Nick != "" {
			return i.Member.Nick
		}
		if i.Member.User != nil {
			return userDisplayName(i.Member.User)
		}
	}
	if i.User != nil {
		return userDisplayName(i.User)
	}
	return "Unknown user"
}

func userDisplayName(user *discordgo.User) string {
	if user.GlobalName != "" {
		return user.GlobalName
	}
	return user.Username
}

func optionString(options []*discordgo.ApplicationCommandInteractionDataOption, name string) string {
	for _, option := range options {
		if option.Name == name {
			return option.StringValue()
		}
	}
	return ""
}

func respond(s *discordgo.Session, i *discordgo.InteractionCreate, title, description string) error {
	if len(description) > 4000 {
		description = description[:4000] + "\n..."
	}

	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{
				{
					Title:       fmt.Sprintf("%s %s", roachEmote(), title),
					Description: description,
					Color:       embedColor,
					Footer: &discordgo.MessageEmbedFooter{
						Text: fmt.Sprintf("%s Roachbot", roachEmote()),
					},
				},
			},
		},
	})
}

func commandTitle(command string) string {
	switch command {
	case "catch-a-roach":
		return "Catch a Roach"
	case "feed-the-roach":
		return "Feed the Roach"
	case "pet-the-roach":
		return "Pet the Roach"
	case "check-roach-mood":
		return "Roach Mood"
	case "check-roach-profile":
		return "Roach Profile"
	case "check-past-roaches":
		return "Past Roaches"
	default:
		return "Roachbot"
	}
}

func roachEmote() string {
	emote := strings.TrimSpace(os.Getenv("ROACH_EMOTE"))
	if emote == "" {
		return defaultRoachEmote
	}
	return emote
}

func cleanRoachName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.Join(strings.Fields(name), " ")
	if len(name) > 40 {
		name = name[:40]
	}
	return name
}

func loadDotEnv(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key != "" {
			_ = os.Setenv(key, value)
		}
	}
}

func bold(value string) string {
	return "**" + strings.ReplaceAll(value, "*", "") + "**"
}

func nullableTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
