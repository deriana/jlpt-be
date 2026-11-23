package sqlite

import (
	"database/sql"
	"errors"

	"jlptmaster/backend/internal/domain"
)

type UserRepository struct {
	DB *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{DB: db}
}

func (r *UserRepository) GetByID(id int64) (*domain.User, error) {
	u := &domain.User{}
	var offline int
	err := r.DB.QueryRow(`
		SELECT id, username, email, password_hash, jlpt_target_level, daily_goal_mins, preferred_voice, offline_mode, streak_days, last_study_date, created_at
		FROM users WHERE id = ?
	`, id).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.JLPTTargetLevel, &u.DailyGoalMins, &u.PreferredVoice, &offline, &u.StreakDays, &u.LastStudyDate, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	u.OfflineMode = offline == 1
	return u, nil
}

func (r *UserRepository) GetByUsernameOrEmail(ident string) (*domain.User, error) {
	u := &domain.User{}
	var offline int
	err := r.DB.QueryRow(`
		SELECT id, username, email, password_hash, jlpt_target_level, daily_goal_mins, preferred_voice, offline_mode, streak_days, last_study_date, created_at
		FROM users WHERE username = ? OR email = ? LIMIT 1
	`, ident, ident).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.JLPTTargetLevel, &u.DailyGoalMins, &u.PreferredVoice, &offline, &u.StreakDays, &u.LastStudyDate, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	u.OfflineMode = offline == 1
	return u, nil
}

func (r *UserRepository) GetDefaultUser() (*domain.User, error) {
	u := &domain.User{}
	var offline int
	err := r.DB.QueryRow(`
		SELECT id, username, email, password_hash, jlpt_target_level, daily_goal_mins, preferred_voice, offline_mode, streak_days, last_study_date, created_at
		FROM users ORDER BY id ASC LIMIT 1
	`).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.JLPTTargetLevel, &u.DailyGoalMins, &u.PreferredVoice, &offline, &u.StreakDays, &u.LastStudyDate, &u.CreatedAt)
	if err != nil {
		return nil, errors.New("no default user found")
	}
	u.OfflineMode = offline == 1
	return u, nil
}

func (r *UserRepository) Create(u *domain.User) error {
	offline := 0
	if u.OfflineMode {
		offline = 1
	}
	res, err := r.DB.Exec(`
		INSERT INTO users (username, email, password_hash, jlpt_target_level, daily_goal_mins, preferred_voice, offline_mode, streak_days)
		VALUES (?, ?, ?, ?, ?, ?, ?, 1)
	`, u.Username, u.Email, u.PasswordHash, u.JLPTTargetLevel, u.DailyGoalMins, u.PreferredVoice, offline)
	if err != nil {
		return err
	}
	u.ID, _ = res.LastInsertId()
	return nil
}

func (r *UserRepository) UpdateProfile(u *domain.User) error {
	offline := 0
	if u.OfflineMode {
		offline = 1
	}
	if u.PasswordHash != "" {
		_, err := r.DB.Exec(`
			UPDATE users SET username = ?, email = ?, password_hash = ?, jlpt_target_level = ?, daily_goal_mins = ?, preferred_voice = ?, offline_mode = ?
			WHERE id = ?
		`, u.Username, u.Email, u.PasswordHash, u.JLPTTargetLevel, u.DailyGoalMins, u.PreferredVoice, offline, u.ID)
		return err
	}
	_, err := r.DB.Exec(`
		UPDATE users SET username = ?, email = ?, jlpt_target_level = ?, daily_goal_mins = ?, preferred_voice = ?, offline_mode = ?
		WHERE id = ?
	`, u.Username, u.Email, u.JLPTTargetLevel, u.DailyGoalMins, u.PreferredVoice, offline, u.ID)
	return err
}
