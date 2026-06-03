# Roachbot

A quirky Discord bot for raising one adorable pet cockroach at a time.

Responses are sent as embeds with a cockroach emote. Set `ROACH_EMOTE` in `.env` to use a custom Discord emote; otherwise Roachbot uses `🪳`.

## Commands

- `/catch-a-roach` catches a new roach if you do not already have a living one.
- `/feed-the-roach` attempts to feed your roach and shows the result with health.
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

## Run on a Linux VM with systemd

From macOS, build a Linux binary:

```sh
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o roachbot-linux .
```

Copy `roachbot-linux` to the VM's `~/Roachbot` directory, then SSH into the VM and run:

```sh
cd ~/Roachbot
chmod +x roachbot-linux
```

Create the service:

```sh
sudo nano /etc/systemd/system/roachbot.service
```

Paste:

```ini
[Unit]
Description=Roachbot Discord Bot
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=phegde04
WorkingDirectory=/home/phegde04/Roachbot
ExecStart=/home/phegde04/Roachbot/roachbot-linux
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Enable and start the bot:

```sh
sudo systemctl daemon-reload
sudo systemctl enable roachbot
sudo systemctl start roachbot
```

Check status and logs:

```sh
systemctl status roachbot
journalctl -u roachbot -f
```
