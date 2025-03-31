package database

import (
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
	log "github.com/sirupsen/logrus"

	"beatbot/config"
	"beatbot/models"
)

func LoadDatabase() (*sql.DB, error) {
	if !config.Config.Database.Enabled {
		return nil, nil
	}

	db, err := sql.Open("sqlite3", config.Config.Database.Path)
	if err != nil {
		return nil, err
	}

	// guild_settings table
	_, err = db.Exec(`
	CREATE TABLE IF NOT EXISTS guild_settings (
		guild_id TEXT NOT NULL,
		tone TEXT,
		volume INTEGER,
		PRIMARY KEY (guild_id)
	)
	`)

	if err != nil {
		return nil, err
	}

	return db, nil
}

func GetGuildSettings(db *sql.DB, guildID string) (models.GuildSettings, error) {
	if !config.Config.Database.Enabled {
		return models.GuildSettings{}, nil
	}

	var settings models.GuildSettings
	err := db.QueryRow("SELECT tone, volume FROM guild_settings WHERE guild_id = ?", guildID).Scan(&settings.Tone, &settings.Volume)
	if err != nil {
		return models.GuildSettings{}, err
	}
	return settings, nil
}

func SetGuildTone(db *sql.DB, guildID string, tone string) error {
	if !config.Config.Database.Enabled {
		return nil
	}

	existing, err := GetGuildSettings(db, guildID)

	if err != nil {
		_, err = db.Exec("INSERT INTO guild_settings (guild_id, tone, volume) VALUES (?, ?, ?)", guildID, tone, 100)
	} else if existing.Tone != tone {
		_, err = db.Exec("UPDATE guild_settings SET tone = ? WHERE guild_id = ?", tone, guildID)
	}

	if err != nil {
		log.Errorf("Error setting guild tone for %s to %s: %s", guildID, tone, err)
	} else {
		log.Debugf("Set guild tone for %s to %s", guildID, tone)
	}
	return err
}

func SetGuildVolume(db *sql.DB, guildID string, volume int) error {
	if !config.Config.Database.Enabled {
		return nil
	}

	existing, err := GetGuildSettings(db, guildID)

	if err != nil {
		_, err = db.Exec("INSERT INTO guild_settings (guild_id, tone, volume) VALUES (?, ?, ?)", guildID, "", volume)
	} else if existing.Volume != volume {
		_, err = db.Exec("UPDATE guild_settings SET volume = ? WHERE guild_id = ?", volume, guildID)
	}

	if err != nil {
		log.Errorf("Error setting guild volume for %s to %d: %s", guildID, volume, err)
	} else {
		log.Debugf("Set guild volume for %s to %d", guildID, volume)
	}
	return err
}
