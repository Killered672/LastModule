package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"calc_service/internal/orchestrator"
	_ "github.com/mattn/go-sqlite3"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
)

type Storage struct {
	db *sql.DB
}

func (s *Storage) CreateUser(login, password string) (int, error) {
	var id int
	err := s.db.QueryRow(
		"INSERT INTO users (login, password) VALUES (?, ?) RETURNING id",
		login, password,
	).Scan(&id)

	if err != nil {
		if isDuplicate(err) {
			return 0, ErrAlreadyExists
		}
		return 0, fmt.Errorf("create user: %w", err)
	}
	return id, nil
}

func (s *Storage) GetUserByLogin(login string) (*User, error) {
	u := &User{}
	err := s.db.QueryRow(
		"SELECT id, login, password FROM users WHERE login = ?",
		login,
	).Scan(&u.ID, &u.Login, &u.Password)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get user: %w", err)
	}
	return u, nil
}

func (s *Storage) CreateExpression(userID int, expr string) (*orchestrator.Expression, error) {
	e := &orchestrator.Expression{
		UserID:     userID,
		Expression: expr,
		Status:     "pending",
		CreatedAt:  time.Now(),
	}

	err := s.db.QueryRow(
		`INSERT INTO expressions 
		(user_id, expression, status, created_at) 
		VALUES (?, ?, ?, ?) 
		RETURNING id`,
		e.UserID, e.Expression, e.Status, e.CreatedAt,
	).Scan(&e.ID)

	if err != nil {
		return nil, fmt.Errorf("create expression: %w", err)
	}
	return e, nil
}

func (s *Storage) GetExpressions(userID int) ([]*orchestrator.Expression, error) {
	rows, err := s.db.Query(
		`SELECT id, expression, status, result, created_at 
		FROM expressions 
		WHERE user_id = ? 
		ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("get expressions: %w", err)
	}
	defer rows.Close()

	var exprs []*orchestrator.Expression
	for rows.Next() {
		e := &orchestrator.Expression{UserID: userID}
		var result sql.NullFloat64
		err := rows.Scan(&e.ID, &e.Expression, &e.Status, &result, &e.CreatedAt)
		if err != nil {
			return nil, err
		}
		if result.Valid {
			e.Result = &result.Float64
		}
		exprs = append(exprs, e)
	}
	return exprs, nil
}

func (s *Storage) UpdateExpression(e *orchestrator.Expression) error {
	var result interface{}
	if e.Result != nil {
		result = *e.Result
	}

	_, err := s.db.Exec(
		`UPDATE expressions 
		SET status = ?, result = ? 
		WHERE id = ? AND user_id = ?`,
		e.Status, result, e.ID, e.UserID,
	)
	return err
}

func (s *Storage) CreateTask(t *orchestrator.Task) error {
	_, err := s.db.Exec(
		`INSERT INTO tasks 
		(expression_id, arg1, arg2, operation, operation_time) 
		VALUES (?, ?, ?, ?, ?)`,
		t.ExprID, t.Arg1, t.Arg2, t.Operation, t.OperationTime,
	)
	return err
}

func (s *Storage) GetPendingTask() (*orchestrator.Task, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	t := &orchestrator.Task{}
	err = tx.QueryRow(
		`SELECT t.id, t.expression_id, t.arg1, t.arg2, t.operation, t.operation_time 
		FROM tasks t
		JOIN expressions e ON t.expression_id = e.id
		WHERE t.completed = FALSE
		ORDER BY e.created_at ASC, t.id ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED`).Scan(
		&t.ID, &t.ExprID, &t.Arg1, &t.Arg2, &t.Operation, &t.OperationTime,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	_, err = tx.Exec(
		"UPDATE tasks SET started_at = NOW() WHERE id = ?",
		t.ID,
	)
	if err != nil {
		return nil, err
	}

	err = tx.Commit()
	return t, err
}

func (s *Storage) CompleteTask(taskID string, result float64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var exprID int
	err = tx.QueryRow(
		"UPDATE tasks SET completed = TRUE, result = ? WHERE id = ? RETURNING expression_id",
		result, taskID,
	).Scan(&exprID)
	if err != nil {
		return err
	}

	var pendingTasks int
	err = tx.QueryRow(
		"SELECT COUNT(*) FROM tasks WHERE expression_id = ? AND completed = FALSE",
		exprID,
	).Scan(&pendingTasks)
	if err != nil {
		return err
	}

	if pendingTasks == 0 {
		_, err = tx.Exec(
			"UPDATE expressions SET status = 'completed' WHERE id = ?",
			exprID,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func isDuplicate(err error) bool {
	return err != nil && err.Error() == "UNIQUE constraint failed: users.login"
}

func NewStorage(dbPath string) (*Storage, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	storage := &Storage{db: db}

	if err := storage.Migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return storage, nil
}

func (s *Storage) Init() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			login TEXT NOT NULL UNIQUE,
			password TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS expressions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			expression TEXT NOT NULL,
			status TEXT NOT NULL,
			result REAL,
			FOREIGN KEY(user_id) REFERENCES users(id)
		);

		CREATE TABLE IF NOT EXISTS tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			expression_id INTEGER NOT NULL,
			arg1 REAL NOT NULL,
			arg2 REAL NOT NULL,
			operation TEXT NOT NULL,
			operation_time INTEGER NOT NULL,
			completed BOOLEAN DEFAULT FALSE,
			FOREIGN KEY(expression_id) REFERENCES expressions(id)
		);
	`)
	return err
}

// Добавьте методы для работы с пользователями, выражениями и задачами
