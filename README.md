# Roachbot

A quirky Discord bot for raising one adorable pet cockroach at a time.

## Commands

- `/catch-a-roach` catches a new roach if you do not already have a living one.
- `/feed-the-roach` attempts to feed your roach. It starts at an 8/10 per-attempt success rate, drops to 7/10 after 5 successful feeds, and keeps dropping by 1/10 every 5 successful feeds. A great mood cancels the age penalty.
- `/pet-the-roach` improves your roach's mood.
- `/check-roach-mood` shows the current mood.
- `/check-roach-profile` displays your living roach's data.
- `/check-past-roaches` lists your dead roaches.

Roaches live up to 30 successful feedings. After every 3 successful feeds, a roach gets full for 4 hours; feeding during that hidden cooldown is rejected before any probability roll. The bot stores everything in the single `roach_master` table in `roachbot.db`.

## Run

1. Create a Discord application and bot, then invite it with `bot` and `applications.commands` scopes.
2. Copy `.env.example` to `.env` and fill in `DISCORD_TOKEN`.
3. Run:

```sh
go mod tidy
go run .
```

For quick command updates while developing, set `DISCORD_GUILD_ID` in `.env`.
