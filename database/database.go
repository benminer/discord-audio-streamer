package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"
	_ "modernc.org/sqlite"
)

type Database struct {
	db      *sql.DB
	session *discordgo.Session
}

type SongHistoryRecord struct {
	ID                  int64
	GuildID             string
	VideoID             string
	Title               string
	URL                 string
	RequestedByUserID   string
	RequestedByUsername string
	PlayedAt            time.Time
	DurationSeconds     int
}

type MostPlayedRecord struct {
	VideoID    string
	Title      string
	URL        string
	PlayCount  int
	LastPlayed time.Time
}

// New creates a new Database instance. dbPath defaults to DB_PATH env var or /app/data/beatbot.db.
func New() (*Database, error) {
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "/app/data/beatbot.db"
	}

	// Ensure parent directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory %s: %w", dir, err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable WAL mode for better concurrent read performance
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set WAL mode: %w", err)
	}

	d := &Database{db: db}
	if err := d.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	log.Infof("Database initialized at %s", dbPath)
	return d, nil
}

func (d *Database) Close() error {
	return d.db.Close()
}

func (d *Database) migrate() error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS song_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			guild_id TEXT NOT NULL,
			video_id TEXT NOT NULL,
			title TEXT NOT NULL,
			url TEXT NOT NULL DEFAULT '',
			requested_by_user_id TEXT NOT NULL DEFAULT '',
			requested_by_username TEXT NOT NULL DEFAULT '',
			played_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			duration_seconds INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_song_history_guild_id ON song_history(guild_id)`,
		`CREATE INDEX IF NOT EXISTS idx_song_history_played_at ON song_history(guild_id, played_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_song_history_video_id ON song_history(guild_id, video_id)`,
		`CREATE TABLE IF NOT EXISTS user_cache (
			user_id TEXT NOT NULL,
			guild_id TEXT NOT NULL,
			username TEXT NOT NULL,
			cached_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (guild_id, user_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_user_cache_lookup ON user_cache(guild_id, user_id)`,
	}

	for _, m := range migrations {
		if _, err := d.db.Exec(m); err != nil {
			return fmt.Errorf("migration failed: %w\nSQL: %s", err, m)
		}
	}

	return nil
}

// RecordPlay inserts a song play record.
func (d *Database) RecordPlay(guildID, videoID, title, url, userID, username string, durationSeconds int) error {
	_, err := d.db.Exec(
		`INSERT INTO song_history (guild_id, video_id, title, url, requested_by_user_id, requested_by_username, played_at, duration_seconds)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		guildID, videoID, title, url, userID, username, time.Now().UTC(), durationSeconds,
	)
	if err != nil {
		return fmt.Errorf("failed to record play: %w", err)
	}
	return nil
}

// GetHistory returns the most recent plays for a guild.
func (d *Database) GetHistory(guildID string, limit int) ([]SongHistoryRecord, error) {
	if limit <= 0 {
		limit = 10
	}

	rows, err := d.db.Query(
		`SELECT id, guild_id, video_id, title, url, requested_by_user_id, requested_by_username, played_at, duration_seconds
		 FROM song_history
		 WHERE guild_id = ?
		 ORDER BY played_at DESC
		 LIMIT ?`,
		guildID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query history: %w", err)
	}
	defer rows.Close()

	var records []SongHistoryRecord
	for rows.Next() {
		var r SongHistoryRecord
		if err := rows.Scan(&r.ID, &r.GuildID, &r.VideoID, &r.Title, &r.URL,
			&r.RequestedByUserID, &r.RequestedByUsername, &r.PlayedAt, &r.DurationSeconds); err != nil {
			return nil, fmt.Errorf("failed to scan history row: %w", err)
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

// GetMostPlayed returns the most played songs for a guild.
func (d *Database) GetMostPlayed(guildID string, limit int) ([]MostPlayedRecord, error) {
	if limit <= 0 {
		limit = 10
	}

	rows, err := d.db.Query(
		`SELECT video_id, title, url, COUNT(*) as play_count, MAX(played_at) as last_played
		 FROM song_history
		 WHERE guild_id = ?
		 GROUP BY video_id
		 ORDER BY play_count DESC, last_played DESC
		 LIMIT ?`,
		guildID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query most played: %w", err)
	}
	defer rows.Close()

	var records []MostPlayedRecord
	for rows.Next() {
		var r MostPlayedRecord
		var lastPlayedStr string
		if err := rows.Scan(&r.VideoID, &r.Title, &r.URL, &r.PlayCount, &lastPlayedStr); err != nil {
			return nil, fmt.Errorf("failed to scan most played row: %w", err)
		}

		// Parse SQLite datetime string to time.Time
		// SQLite stores timestamps in various formats depending on how they were inserted
		lastPlayed, err := time.Parse("2006-01-02 15:04:05", lastPlayedStr)
		if err != nil {
			// Try RFC3339 format as fallback
			lastPlayed, err = time.Parse(time.RFC3339, lastPlayedStr)
			if err != nil {
				// Try RFC3339Nano format (used by time.Now().UTC())
				lastPlayed, err = time.Parse(time.RFC3339Nano, lastPlayedStr)
				if err != nil {
					log.Warnf("failed to parse last_played timestamp '%s': %v", lastPlayedStr, err)
					lastPlayed = time.Time{} // Use zero time if parsing fails
				}
			}
		}
		r.LastPlayed = lastPlayed

		records = append(records, r)
	}
	return records, rows.Err()
}

// SetSession sets the Discord session for username lookups.
func (d *Database) SetSession(session *discordgo.Session) {
	d.session = session
}

// GetOrFetchUsername retrieves a username from cache or fetches from Discord API if not found or stale.
// Stale threshold is 7 days. Returns "Unknown" if all attempts fail.
func (d *Database) GetOrFetchUsername(guildID, userID string) string {
	if userID == "" {
		return "Unknown"
	}

	// First, try to get from cache
	var username string
	var cachedAt time.Time
	err := d.db.QueryRow(
		`SELECT username, cached_at FROM user_cache WHERE guild_id = ? AND user_id = ?`,
		guildID, userID,
	).Scan(&username, &cachedAt)

	// If found in cache and not stale (< 7 days old), return it
	if err == nil {
		if time.Since(cachedAt) < 7*24*time.Hour {
			return username
		}
	}

	// Not in cache or stale - try to fetch from Discord API
	if d.session != nil {
		member, err := d.session.GuildMember(guildID, userID)
		if err == nil {
			// Prefer nickname over username
			username = member.User.Username
			if member.Nick != "" {
				username = member.Nick
			}

			// Update cache
			_, err = d.db.Exec(
				`INSERT OR REPLACE INTO user_cache (guild_id, user_id, username, cached_at) VALUES (?, ?, ?, ?)`,
				guildID, userID, username, time.Now().UTC(),
			)
			if err != nil {
				log.Warnf("Failed to update user cache for %s/%s: %v", guildID, userID, err)
			}

			return username
		}

		// If guild member lookup failed, try user lookup as fallback
		user, err := d.session.User(userID)
		if err == nil {
			username = user.Username

			// Update cache
			_, err = d.db.Exec(
				`INSERT OR REPLACE INTO user_cache (guild_id, user_id, username, cached_at) VALUES (?, ?, ?, ?)`,
				guildID, userID, username, time.Now().UTC(),
			)
			if err != nil {
				log.Warnf("Failed to update user cache for %s/%s: %v", guildID, userID, err)
			}

			return username
		}

		log.Warnf("Failed to fetch user from Discord API for %s/%s: %v", guildID, userID, err)
	}

	// If we have a stale cached value, return it rather than "Unknown"
	if err == nil && username != "" {
		return username
	}

	return "Unknown"
}
