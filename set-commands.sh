source .env

# For debugging - print the token and application ID
echo "Using Application ID: $DISCORD_APP_ID"
echo "Using Bot Token: $DISCORD_BOT_TOKEN"
# Make sure we can read the commands.json file
echo "Contents of commands.json:"
cat commands.json

# Send the PUT request (changed from POST to PUT)
curl -X PUT \
  -H "Authorization: Bot $DISCORD_BOT_TOKEN" \
  -H "Content-Type: application/json" \
  -d @commands.json \
  "https://discord.com/api/v10/applications/$DISCORD_APP_ID/commands"