# Discord Interaction ID Unmarshal Fix

## Issue
- Error: \"Error unmarshalling interaction: json: cannot unmarshal number into Go struct field InteractionData.data.id of type string\"
- Log: [GIN] POST \"/discord/interactions\"
- Root cause: Discord sends `interaction.data.id` as JSON number (snowflake ID), but Go `InteractionData.ID string \`json:\"id\"\`` expects string without number-to-string conversion.

## Fix Applied
- File: `handlers/handlers.go`
- Change: `ID string \`json:\"id\"\`` → `ID string \`json:\"id,string\"\``
- Effect: Go's json decoder parses numeric values as strings via the `,string` tag.
- Commit: fix(discord): add \`json:\\\"id,string\\\"\` tag...

## Deployment
```bash
cd ~/eva-apps/projects/discord-audio-streamer
./docker-build.sh && ./docker-restart.sh rebuild
```

## Verification
- Hit any Discord button → No unmarshal error in logs
- Check: `./docker-logs.sh | grep interaction`
- Current git status: clean on mac-mini-optimize branch