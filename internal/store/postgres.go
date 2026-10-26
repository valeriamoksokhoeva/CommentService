package store

import (
	"comment_service/internal/domain"
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jackc/pgx/v5"
)

type PostgresStore struct {
	db *pgxpool.Pool
}

func NewPostgresStore(ctx context.Context, databaseURL string) (*PostgresStore, error) {
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}

	if err := db.Ping(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return &PostgresStore{db: db}, nil
}

func (s *PostgresStore) Close() {
	s.db.Close()
}

func (s *PostgresStore) CreatePost(ctx context.Context, post domain.Post) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO posts (id, message, author, can_comment)
		 VALUES ($1, $2, $3, $4)`,
		post.ID, post.Message, post.Author, post.CanComment,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return domain.ErrPostExists
		}
		return fmt.Errorf("create post: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetPost(ctx context.Context, postID string) (domain.Post, error) {
	var post domain.Post

	err := s.db.QueryRow(ctx, `
		SELECT id, message, author, can_comment
		FROM posts
		WHERE id = $1
	`, postID).Scan(
		&post.ID,
		&post.Message,
		&post.Author,
		&post.CanComment,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Post{}, domain.ErrPostNotFound
	}

	if err != nil {
		return domain.Post{}, fmt.Errorf("get post: %w", err)
	}

	return post, nil
}

func (s *PostgresStore) Posts(ctx context.Context) ([]domain.Post, error) {
	rows, err := s.db.Query(
		ctx,
		`SELECT id, message, author, can_comment
		 FROM posts ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("query posts: %w", err)
	}
	defer rows.Close()

	posts := make([]domain.Post, 0)

	for rows.Next() {
		var post domain.Post

		if err := rows.Scan(
			&post.ID,
			&post.Message,
			&post.Author,
			&post.CanComment,
		); err != nil {
			return nil, fmt.Errorf("scan post: %w", err)
		}

		posts = append(posts, post)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate posts: %w", err)
	}

	return posts, nil
}

func (s *PostgresStore) CreateComment(ctx context.Context, comment domain.Comment) error { // !!!транзакция
	if strings.TrimSpace(comment.Message) == "" {
		return domain.ErrEmptyMessage
	}

	if strings.TrimSpace(comment.Author) == "" {
		return domain.ErrEmptyAuthor
	}
	if utf8.RuneCountInString(comment.Message) > 2000 {
		return domain.ErrCommentTooLong
	}

	var canComment bool

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx,
		`SELECT can_comment FROM posts WHERE id = $1 FOR SHARE`,
		comment.PostID,
	).Scan(&canComment)

	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrPostNotFound
	}
	if err != nil {
		return fmt.Errorf("get post for comment: %w", err)
	}

	if !canComment {
		return domain.ErrCommentsForbidden
	}
	if comment.ParentID != nil {
		var parentPostID string

		err = tx.QueryRow(ctx,
			`SELECT post_id FROM comments WHERE id = $1`,
			*comment.ParentID,
		).Scan(&parentPostID)

		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrCommentNotFound
		}
		if err != nil {
			return fmt.Errorf("get parent comment: %w", err)
		}

		if parentPostID != comment.PostID {
			return domain.ErrCommentsNoMatch
		}
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO comments (id, post_id, parent_id, message, author)
		VALUES ($1, $2, $3, $4, $5)
	`,
		comment.ID,
		comment.PostID,
		comment.ParentID,
		comment.Message,
		comment.Author,
	)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return domain.ErrCommentExists
		}

		return fmt.Errorf("create comment: %w", err)
	}

	return tx.Commit(ctx)
}

func (s *PostgresStore) GetCommentTree(ctx context.Context, postID string, limit int, offset int) ([]domain.CommentTree, error) {
	_, err := s.GetPost(ctx, postID)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.Query(
		ctx,
		`WITH RECURSIVE roots AS (
			SELECT id, post_id, parent_id, message, author, created_at
			FROM comments
			WHERE post_id = $1 AND parent_id IS NULL
			ORDER BY created_at, id
			LIMIT $2 OFFSET $3
		),
		tree AS (
			SELECT * FROM roots
			UNION ALL
			SELECT c.id, c.post_id, c.parent_id, c.message, c.author, c.created_at
			FROM comments c
			JOIN tree t ON c.parent_id = t.id
		)
		SELECT id, post_id, parent_id, message, author
		FROM tree
		ORDER BY created_at, id`,

		postID, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("get comment tree: %w", err)
	}
	defer rows.Close()

	comments := make([]domain.Comment, 0)

	for rows.Next() {
		var comment domain.Comment

		if err := rows.Scan(
			&comment.ID,
			&comment.PostID,
			&comment.ParentID,
			&comment.Message,
			&comment.Author,
		); err != nil {
			return nil, fmt.Errorf("scan comment: %w", err)
		}

		comments = append(comments, comment)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate comments: %w", err)
	}

	nodes := make(map[string]*domain.CommentTree, len(comments))

	for _, comment := range comments {
		nodes[comment.ID] = &domain.CommentTree{
			Comment: comment,
			Replies: make([]*domain.CommentTree, 0),
		}
	}

	for _, comment := range comments {
		if comment.ParentID == nil {
			continue
		}

		parent := nodes[*comment.ParentID]
		if parent != nil {
			parent.Replies = append(parent.Replies, nodes[comment.ID])
		}
	}
	roots := make([]domain.CommentTree, 0)

	for _, comment := range comments {
		if comment.ParentID == nil {
			roots = append(roots, *nodes[comment.ID])
		}
	}
	return roots, nil
}

func (s *PostgresStore) DisableComments(ctx context.Context, postID string, author string) (domain.Post, error) {
	var post domain.Post

	err := s.db.QueryRow(
		ctx,
		`UPDATE posts
		 SET can_comment = FALSE
		 WHERE id = $1 and author = $2
		 RETURNING id, message, author, can_comment`,
		postID, author,
	).Scan(
		&post.ID,
		&post.Message,
		&post.Author,
		&post.CanComment,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		if _, getErr := s.GetPost(ctx, postID); getErr != nil {
			return domain.Post{}, getErr
		}
		return domain.Post{}, domain.ErrNotPostAuthor
	}

	if err != nil {
		return domain.Post{}, fmt.Errorf("disable comments: %w", err)
	}

	return post, nil
}
