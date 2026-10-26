package store

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"comment_service/internal/domain"

	"github.com/google/uuid"
)

// Интеграционные тесты: нужен PostgreSQL и отдельная база из TEST_DATABASE_URL.
// Без TEST_DATABASE_URL все тесты из этого файла пропускаются.
// Миграции применяются к тестовой базе автоматически. Каждый тест создаёт
// данные с уникальными UUID, поэтому тесты не мешают друг другу.

func newTestPostgresStore(t *testing.T) *PostgresStore {
	t.Helper()

	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping PostgreSQL tests")
	}

	s, err := NewPostgresStore(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("NewPostgresStore() error: %v", err)
	}

	t.Cleanup(s.Close)

	applyTestMigrations(t, s)
	return s
}

func applyTestMigrations(t *testing.T, s *PostgresStore) {
	t.Helper()

	migration, err := os.ReadFile("../../migrations/001_init.sql")
	if err != nil {
		t.Fatalf("read test migration: %v", err)
	}

	// The migration contains independent CREATE TABLE/INDEX statements.
	for _, statement := range strings.Split(string(migration), ";") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if _, err := s.db.Exec(context.Background(), statement); err != nil {
			t.Fatalf("apply test migration: %v", err)
		}
	}
}

func pgCreatePost(t *testing.T, s *PostgresStore, canComment bool) domain.Post {
	t.Helper()

	post := domain.Post{
		ID:         uuid.NewString(),
		Message:    "Test post",
		Author:     "Alice",
		CanComment: canComment,
	}
	if err := s.CreatePost(context.Background(), post); err != nil {
		t.Fatalf("CreatePost() error: %v", err)
	}
	return post
}

func pgCreateComment(t *testing.T, s *PostgresStore, postID string, parentID *string) domain.Comment {
	t.Helper()

	comment := domain.Comment{
		ID:       uuid.NewString(),
		PostID:   postID,
		ParentID: parentID,
		Message:  "Test comment",
		Author:   "Bob",
	}
	if err := s.CreateComment(context.Background(), comment); err != nil {
		t.Fatalf("CreateComment() error: %v", err)
	}
	return comment
}

func TestPostgresStore_CreateAndGetPost(t *testing.T) {
	s := newTestPostgresStore(t)
	post := pgCreatePost(t, s, true)

	got, err := s.GetPost(context.Background(), post.ID)
	if err != nil {
		t.Fatalf("GetPost() error: %v", err)
	}
	if got != post {
		t.Fatalf("GetPost() = %+v, want %+v", got, post)
	}
}

func TestPostgresStore_CreatePost_Duplicate(t *testing.T) {
	s := newTestPostgresStore(t)
	post := pgCreatePost(t, s, true)

	err := s.CreatePost(context.Background(), post)
	if !errors.Is(err, domain.ErrPostExists) {
		t.Fatalf("error = %v, want %v", err, domain.ErrPostExists)
	}
}

func TestPostgresStore_GetPost_NotFound(t *testing.T) {
	s := newTestPostgresStore(t)

	_, err := s.GetPost(context.Background(), uuid.NewString())
	if !errors.Is(err, domain.ErrPostNotFound) {
		t.Fatalf("error = %v, want %v", err, domain.ErrPostNotFound)
	}
}

func TestPostgresStore_Posts(t *testing.T) {
	s := newTestPostgresStore(t)
	p1 := pgCreatePost(t, s, true)
	p2 := pgCreatePost(t, s, false)

	posts, err := s.Posts(context.Background())
	if err != nil {
		t.Fatalf("Posts() error: %v", err)
	}

	// В базе могут быть посты из других тестов, поэтому ищем свои.
	found := make(map[string]domain.Post)
	for _, post := range posts {
		found[post.ID] = post
	}
	for _, want := range []domain.Post{p1, p2} {
		if got, ok := found[want.ID]; !ok || got != want {
			t.Fatalf("post %s: got %+v (found=%v), want %+v", want.ID, got, ok, want)
		}
	}
}

func TestPostgresStore_DisableComments(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgresStore(t)
	post := pgCreatePost(t, s, true)

	tests := []struct {
		name    string
		postID  string
		author  string
		wantErr error
	}{
		{"another user", post.ID, "Bob", domain.ErrNotPostAuthor},
		{"author name is case sensitive", post.ID, "alice", domain.ErrNotPostAuthor},
		{"missing post", uuid.NewString(), "Alice", domain.ErrPostNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.DisableComments(ctx, tc.postID, tc.author); !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
		})
	}

	saved, err := s.GetPost(ctx, post.ID)
	if err != nil {
		t.Fatalf("GetPost() error: %v", err)
	}
	if !saved.CanComment {
		t.Fatal("comments were disabled by a failed DisableComments call")
	}

	got, err := s.DisableComments(ctx, post.ID, "Alice")
	if err != nil {
		t.Fatalf("DisableComments() by author error: %v", err)
	}
	if got.CanComment {
		t.Fatal("DisableComments() returned CanComment = true")
	}
	saved, err = s.GetPost(ctx, post.ID)
	if err != nil {
		t.Fatalf("GetPost() error: %v", err)
	}
	if saved.CanComment {
		t.Fatal("stored CanComment = true after DisableComments")
	}
}

func TestPostgresStore_CreateComment_Errors(t *testing.T) {
	tests := []struct {
		name     string
		disabled bool
		mutate   func(c *domain.Comment)
		want     error
	}{
		{
			name:   "post not found",
			mutate: func(c *domain.Comment) { c.PostID = uuid.NewString() },
			want:   domain.ErrPostNotFound,
		},
		{
			name:     "comments disabled",
			disabled: true,
			mutate:   func(c *domain.Comment) {},
			want:     domain.ErrCommentsForbidden,
		},
		{
			name:   "empty message",
			mutate: func(c *domain.Comment) { c.Message = "" },
			want:   domain.ErrEmptyMessage,
		},
		{
			name:   "whitespace message",
			mutate: func(c *domain.Comment) { c.Message = " \n\t " },
			want:   domain.ErrEmptyMessage,
		},
		{
			name:   "empty author",
			mutate: func(c *domain.Comment) { c.Author = "" },
			want:   domain.ErrEmptyAuthor,
		},
		{
			name:   "whitespace author",
			mutate: func(c *domain.Comment) { c.Author = "   " },
			want:   domain.ErrEmptyAuthor,
		},
		{
			name:   "message too long",
			mutate: func(c *domain.Comment) { c.Message = strings.Repeat("я", 2001) },
			want:   domain.ErrCommentTooLong,
		},
		{
			name:   "parent not found",
			mutate: func(c *domain.Comment) { c.ParentID = stringPtr(uuid.NewString()) },
			want:   domain.ErrCommentNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s := newTestPostgresStore(t)
			post := pgCreatePost(t, s, !tc.disabled)

			comment := domain.Comment{ID: uuid.NewString(), PostID: post.ID, Message: "hello", Author: "Bob"}
			tc.mutate(&comment)

			err := s.CreateComment(ctx, comment)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}

			tree, err := s.GetCommentTree(ctx, post.ID, 10, 0)
			if err != nil {
				t.Fatalf("GetCommentTree() error: %v", err)
			}
			if len(tree) != 0 {
				t.Fatalf("stored %d comments after a failed CreateComment, want 0", len(tree))
			}
		})
	}
}

func TestPostgresStore_CreateComment_MaxLength(t *testing.T) {
	// Проверка в Go считает символы, CHECK в базе тоже: 2000 букв "я" должны пройти оба.
	s := newTestPostgresStore(t)
	post := pgCreatePost(t, s, true)

	err := s.CreateComment(context.Background(), domain.Comment{
		ID:      uuid.NewString(),
		PostID:  post.ID,
		Message: strings.Repeat("я", 2000),
		Author:  "Bob",
	})
	if err != nil {
		t.Fatalf("2000 characters must be accepted, got %v", err)
	}
}

func TestPostgresStore_CreateComment_Duplicate(t *testing.T) {
	s := newTestPostgresStore(t)
	post := pgCreatePost(t, s, true)
	comment := pgCreateComment(t, s, post.ID, nil)

	err := s.CreateComment(context.Background(), comment)
	if !errors.Is(err, domain.ErrCommentExists) {
		t.Fatalf("error = %v, want %v", err, domain.ErrCommentExists)
	}
}

func TestPostgresStore_CreateComment_ParentFromAnotherPost(t *testing.T) {
	s := newTestPostgresStore(t)
	p1 := pgCreatePost(t, s, true)
	p2 := pgCreatePost(t, s, true)
	root := pgCreateComment(t, s, p1.ID, nil)

	err := s.CreateComment(context.Background(), domain.Comment{
		ID:       uuid.NewString(),
		PostID:   p2.ID,
		ParentID: &root.ID,
		Message:  "reply",
		Author:   "Bob",
	})
	if !errors.Is(err, domain.ErrCommentsNoMatch) {
		t.Fatalf("error = %v, want %v", err, domain.ErrCommentsNoMatch)
	}
}

func TestPostgresStore_GetCommentTree_PostNotFound(t *testing.T) {
	s := newTestPostgresStore(t)

	_, err := s.GetCommentTree(context.Background(), uuid.NewString(), 10, 0)
	if !errors.Is(err, domain.ErrPostNotFound) {
		t.Fatalf("error = %v, want %v", err, domain.ErrPostNotFound)
	}
}

func TestPostgresStore_GetCommentTree_NoComments(t *testing.T) {
	s := newTestPostgresStore(t)
	post := pgCreatePost(t, s, true)

	tree, err := s.GetCommentTree(context.Background(), post.ID, 10, 0)
	if err != nil {
		t.Fatalf("GetCommentTree() error: %v", err)
	}
	if tree == nil || len(tree) != 0 {
		t.Fatalf("tree = %#v, want empty non-nil slice", tree)
	}
}

func TestPostgresStore_GetCommentTree_Nesting(t *testing.T) {
	s := newTestPostgresStore(t)
	post := pgCreatePost(t, s, true)

	c1 := pgCreateComment(t, s, post.ID, nil)
	c2 := pgCreateComment(t, s, post.ID, &c1.ID)
	c3 := pgCreateComment(t, s, post.ID, &c2.ID)
	c4 := pgCreateComment(t, s, post.ID, &c1.ID)

	tree, err := s.GetCommentTree(context.Background(), post.ID, 10, 0)
	if err != nil {
		t.Fatalf("GetCommentTree() error: %v", err)
	}

	if got := rootIDs(tree); !slices.Equal(got, []string{c1.ID}) {
		t.Fatalf("roots = %v, want [%s]", got, c1.ID)
	}
	root := tree[0]
	if got := replyIDs(&root); !slices.Equal(got, []string{c2.ID, c4.ID}) {
		t.Fatalf("replies of c1 = %v, want [%s %s]", got, c2.ID, c4.ID)
	}
	if got := replyIDs(root.Replies[0]); !slices.Equal(got, []string{c3.ID}) {
		t.Fatalf("replies of c2 = %v, want [%s]", got, c3.ID)
	}
}

func TestPostgresStore_GetCommentTree_DeepNesting(t *testing.T) {
	const depth = 200

	s := newTestPostgresStore(t)
	post := pgCreatePost(t, s, true)

	var parent *string
	for i := 0; i < depth; i++ {
		comment := pgCreateComment(t, s, post.ID, parent)
		parent = &comment.ID
	}

	tree, err := s.GetCommentTree(context.Background(), post.ID, 10, 0)
	if err != nil {
		t.Fatalf("GetCommentTree() error: %v", err)
	}
	if len(tree) != 1 {
		t.Fatalf("roots = %d, want 1", len(tree))
	}

	levels := 0
	node := &tree[0]
	for {
		levels++
		if len(node.Replies) == 0 {
			break
		}
		node = node.Replies[0]
	}
	if levels != depth {
		t.Fatalf("tree depth = %d, want %d", levels, depth)
	}
}

func TestPostgresStore_GetCommentTree_Pagination(t *testing.T) {
	s := newTestPostgresStore(t)
	post := pgCreatePost(t, s, true)

	roots := make([]string, 5)
	for i := range roots {
		roots[i] = pgCreateComment(t, s, post.ID, nil).ID
	}
	// Ответы не должны занимать место в странице: лимит считается по корням.
	a1 := pgCreateComment(t, s, post.ID, &roots[2])
	a2 := pgCreateComment(t, s, post.ID, &roots[2])

	tests := []struct {
		name          string
		limit, offset int
		want          []string
	}{
		{"first page", 2, 0, roots[0:2]},
		{"middle page", 2, 2, roots[2:4]},
		{"last incomplete page", 2, 4, roots[4:5]},
		{"limit bigger than total", 10, 0, roots},
		{"offset equals total", 10, 5, []string{}},
		{"offset beyond total", 10, 100, []string{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tree, err := s.GetCommentTree(context.Background(), post.ID, tc.limit, tc.offset)
			if err != nil {
				t.Fatalf("GetCommentTree() error: %v", err)
			}
			if got := rootIDs(tree); !slices.Equal(got, tc.want) {
				t.Fatalf("roots = %v, want %v", got, tc.want)
			}
		})
	}

	t.Run("replies come with their root", func(t *testing.T) {
		tree, err := s.GetCommentTree(context.Background(), post.ID, 1, 2)
		if err != nil {
			t.Fatalf("GetCommentTree() error: %v", err)
		}
		if len(tree) != 1 || tree[0].Comment.ID != roots[2] {
			t.Fatalf("roots = %v, want [%s]", rootIDs(tree), roots[2])
		}
		if got := replyIDs(&tree[0]); !slices.Equal(got, []string{a1.ID, a2.ID}) {
			t.Fatalf("replies = %v, want [%s %s]", got, a1.ID, a2.ID)
		}
	})
}

func TestPostgresStore_GetCommentTree_OnlyOwnPost(t *testing.T) {
	s := newTestPostgresStore(t)
	p1 := pgCreatePost(t, s, true)
	p2 := pgCreatePost(t, s, true)
	c1 := pgCreateComment(t, s, p1.ID, nil)
	pgCreateComment(t, s, p2.ID, nil)

	tree, err := s.GetCommentTree(context.Background(), p1.ID, 10, 0)
	if err != nil {
		t.Fatalf("GetCommentTree() error: %v", err)
	}
	if got := rootIDs(tree); !slices.Equal(got, []string{c1.ID}) {
		t.Fatalf("roots = %v, want [%s]", got, c1.ID)
	}
}

// Комментарии пишутся параллельно с запретом. После завершения DisableComments
// ни один новый комментарий не должен быть принят.
func TestPostgresStore_NoCommentsAfterDisable(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgresStore(t)
	post := pgCreatePost(t, s, true)

	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.CreateComment(ctx, domain.Comment{
				ID: uuid.NewString(), PostID: post.ID, Message: "m", Author: "Bob",
			})
			if err != nil && !errors.Is(err, domain.ErrCommentsForbidden) {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	if _, err := s.DisableComments(ctx, post.ID, "Alice"); err != nil {
		t.Fatalf("DisableComments() error: %v", err)
	}
	wg.Wait()

	err := s.CreateComment(ctx, domain.Comment{
		ID: uuid.NewString(), PostID: post.ID, Message: "late", Author: "Bob",
	})
	if !errors.Is(err, domain.ErrCommentsForbidden) {
		t.Fatalf("after disable: error = %v, want %v", err, domain.ErrCommentsForbidden)
	}
}

func TestPostgresStore_ConcurrentCreateAndRead(t *testing.T) {
	ctx := context.Background()
	s := newTestPostgresStore(t)
	post := pgCreatePost(t, s, true)

	const n = 30
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			err := s.CreateComment(ctx, domain.Comment{
				ID: uuid.NewString(), PostID: post.ID, Message: "m", Author: "Bob",
			})
			if err != nil {
				t.Errorf("CreateComment() error: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := s.GetCommentTree(ctx, post.ID, 10, 0); err != nil {
				t.Errorf("GetCommentTree() error: %v", err)
			}
		}()
	}
	wg.Wait()

	tree, err := s.GetCommentTree(ctx, post.ID, n, 0)
	if err != nil {
		t.Fatalf("GetCommentTree() error: %v", err)
	}
	if len(tree) != n {
		t.Fatalf("got %d comments, want %d", len(tree), n)
	}
}
