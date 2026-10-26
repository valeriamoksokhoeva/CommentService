package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"comment_service/internal/domain"
)

func stringPtr(v string) *string {
	return &v
}

// newStoreWithPost возвращает хранилище с одним постом p1 от Alice.
func newStoreWithPost(t *testing.T, canComment bool) *MemoryStore {
	t.Helper()

	s := NewMemoryStore()
	err := s.CreatePost(context.Background(), domain.Post{
		ID:         "p1",
		Message:    "post",
		Author:     "Alice",
		CanComment: canComment,
	})
	if err != nil {
		t.Fatalf("CreatePost() error: %v", err)
	}
	return s
}

// addComment создаёт комментарий в посте p1 и падает, если это не удалось.
func addComment(t *testing.T, s *MemoryStore, id string, parentID *string) {
	t.Helper()

	err := s.CreateComment(context.Background(), domain.Comment{
		ID:       id,
		PostID:   "p1",
		ParentID: parentID,
		Message:  "text " + id,
		Author:   "Bob",
	})
	if err != nil {
		t.Fatalf("CreateComment(%s) error: %v", id, err)
	}
}

func rootIDs(tree []domain.CommentTree) []string {
	ids := make([]string, 0, len(tree))
	for _, node := range tree {
		ids = append(ids, node.Comment.ID)
	}
	return ids
}

func replyIDs(node *domain.CommentTree) []string {
	ids := make([]string, 0, len(node.Replies))
	for _, reply := range node.Replies {
		ids = append(ids, reply.Comment.ID)
	}
	return ids
}

func TestMemoryStore_CreateAndGetPost(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	post := domain.Post{ID: "p1", Message: "hello", Author: "Alice", CanComment: true}

	if err := s.CreatePost(ctx, post); err != nil {
		t.Fatalf("CreatePost() error: %v", err)
	}

	got, err := s.GetPost(ctx, "p1")
	if err != nil {
		t.Fatalf("GetPost() error: %v", err)
	}
	if got != post {
		t.Fatalf("GetPost() = %+v, want %+v", got, post)
	}
}

func TestMemoryStore_CreatePost_Duplicate(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	post := domain.Post{ID: "p1", Message: "hello", Author: "Alice", CanComment: true}

	if err := s.CreatePost(ctx, post); err != nil {
		t.Fatalf("CreatePost() error: %v", err)
	}
	if err := s.CreatePost(ctx, post); !errors.Is(err, domain.ErrPostExists) {
		t.Fatalf("error = %v, want %v", err, domain.ErrPostExists)
	}
}

func TestMemoryStore_GetPost_NotFound(t *testing.T) {
	s := NewMemoryStore()

	_, err := s.GetPost(context.Background(), "missing")
	if !errors.Is(err, domain.ErrPostNotFound) {
		t.Fatalf("error = %v, want %v", err, domain.ErrPostNotFound)
	}
}

func TestMemoryStore_Posts(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	posts, err := s.Posts(ctx)
	if err != nil {
		t.Fatalf("Posts() error: %v", err)
	}
	if len(posts) != 0 {
		t.Fatalf("empty store: got %d posts, want 0", len(posts))
	}

	for _, id := range []string{"p1", "p2", "p3"} {
		if err := s.CreatePost(ctx, domain.Post{ID: id, Message: id, Author: "Alice"}); err != nil {
			t.Fatalf("CreatePost(%s) error: %v", id, err)
		}
	}

	posts, err = s.Posts(ctx)
	if err != nil {
		t.Fatalf("Posts() error: %v", err)
	}

	// Порядок постов в map не гарантирован, поэтому сравниваем отсортированные ID.
	ids := make([]string, 0, len(posts))
	for _, post := range posts {
		ids = append(ids, post.ID)
	}
	slices.Sort(ids)
	if want := []string{"p1", "p2", "p3"}; !slices.Equal(ids, want) {
		t.Fatalf("post IDs = %v, want %v", ids, want)
	}
}

func TestMemoryStore_CreateComment_Errors(t *testing.T) {
	tests := []struct {
		name     string
		disabled bool
		mutate   func(c *domain.Comment)
		want     error
	}{
		{
			name:   "post not found",
			mutate: func(c *domain.Comment) { c.PostID = "missing" },
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
			mutate: func(c *domain.Comment) { c.ParentID = stringPtr("missing") },
			want:   domain.ErrCommentNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s := newStoreWithPost(t, !tc.disabled)

			comment := domain.Comment{ID: "c1", PostID: "p1", Message: "hello", Author: "Bob"}
			tc.mutate(&comment)

			err := s.CreateComment(ctx, comment)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}

			// После ошибки комментарий не должен попасть в хранилище.
			tree, err := s.GetCommentTree(ctx, "p1", 10, 0)
			if err != nil {
				t.Fatalf("GetCommentTree() error: %v", err)
			}
			if len(tree) != 0 {
				t.Fatalf("stored %d comments after a failed CreateComment, want 0", len(tree))
			}
		})
	}
}

func TestMemoryStore_CreateComment_MaxLength(t *testing.T) {
	// Длина считается в символах, а не в байтах: "я" занимает 2 байта.
	for _, letter := range []string{"a", "я"} {
		t.Run(letter, func(t *testing.T) {
			s := newStoreWithPost(t, true)

			err := s.CreateComment(context.Background(), domain.Comment{
				ID:      "c1",
				PostID:  "p1",
				Message: strings.Repeat(letter, 2000),
				Author:  "Bob",
			})
			if err != nil {
				t.Fatalf("2000 characters must be accepted, got %v", err)
			}
		})
	}
}

func TestMemoryStore_CreateComment_Duplicate(t *testing.T) {
	s := newStoreWithPost(t, true)
	addComment(t, s, "c1", nil)

	err := s.CreateComment(context.Background(), domain.Comment{
		ID: "c1", PostID: "p1", Message: "again", Author: "Bob",
	})
	if !errors.Is(err, domain.ErrCommentExists) {
		t.Fatalf("error = %v, want %v", err, domain.ErrCommentExists)
	}
}

func TestMemoryStore_CreateComment_ParentFromAnotherPost(t *testing.T) {
	ctx := context.Background()
	s := newStoreWithPost(t, true)
	if err := s.CreatePost(ctx, domain.Post{ID: "p2", Author: "Alice", CanComment: true}); err != nil {
		t.Fatalf("CreatePost() error: %v", err)
	}
	addComment(t, s, "c1", nil)

	err := s.CreateComment(ctx, domain.Comment{
		ID: "c2", PostID: "p2", ParentID: stringPtr("c1"), Message: "reply", Author: "Bob",
	})
	if !errors.Is(err, domain.ErrCommentsNoMatch) {
		t.Fatalf("error = %v, want %v", err, domain.ErrCommentsNoMatch)
	}
}

func TestMemoryStore_GetCommentTree_PostNotFound(t *testing.T) {
	s := NewMemoryStore()

	_, err := s.GetCommentTree(context.Background(), "missing", 10, 0)
	if !errors.Is(err, domain.ErrPostNotFound) {
		t.Fatalf("error = %v, want %v", err, domain.ErrPostNotFound)
	}
}

func TestMemoryStore_GetCommentTree_NoComments(t *testing.T) {
	s := newStoreWithPost(t, true)

	tree, err := s.GetCommentTree(context.Background(), "p1", 10, 0)
	if err != nil {
		t.Fatalf("GetCommentTree() error: %v", err)
	}
	if tree == nil || len(tree) != 0 {
		t.Fatalf("tree = %#v, want empty non-nil slice", tree)
	}
}

func TestMemoryStore_GetCommentTree_Nesting(t *testing.T) {
	s := newStoreWithPost(t, true)
	addComment(t, s, "c1", nil)
	addComment(t, s, "c2", stringPtr("c1"))
	addComment(t, s, "c3", stringPtr("c2"))
	addComment(t, s, "c4", stringPtr("c1"))

	tree, err := s.GetCommentTree(context.Background(), "p1", 10, 0)
	if err != nil {
		t.Fatalf("GetCommentTree() error: %v", err)
	}

	if got := rootIDs(tree); !slices.Equal(got, []string{"c1"}) {
		t.Fatalf("roots = %v, want [c1]", got)
	}
	root := tree[0]
	if got := replyIDs(&root); !slices.Equal(got, []string{"c2", "c4"}) {
		t.Fatalf("replies of c1 = %v, want [c2 c4]", got)
	}
	if got := replyIDs(root.Replies[0]); !slices.Equal(got, []string{"c3"}) {
		t.Fatalf("replies of c2 = %v, want [c3]", got)
	}
	if len(root.Replies[1].Replies) != 0 {
		t.Fatalf("c4 must have no replies, got %d", len(root.Replies[1].Replies))
	}
}

func TestMemoryStore_GetCommentTree_DeepNesting(t *testing.T) {
	const depth = 500

	s := newStoreWithPost(t, true)
	var parent *string
	for i := 0; i < depth; i++ {
		id := fmt.Sprintf("c%d", i)
		addComment(t, s, id, parent)
		parent = stringPtr(id)
	}

	tree, err := s.GetCommentTree(context.Background(), "p1", 10, 0)
	if err != nil {
		t.Fatalf("GetCommentTree() error: %v", err)
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

func TestMemoryStore_GetCommentTree_Pagination(t *testing.T) {
	s := newStoreWithPost(t, true)
	for _, id := range []string{"r1", "r2", "r3", "r4", "r5"} {
		addComment(t, s, id, nil)
	}
	// Ответы не должны занимать место в странице: лимит считается по корням.
	addComment(t, s, "a1", stringPtr("r3"))
	addComment(t, s, "a2", stringPtr("r3"))

	tests := []struct {
		name          string
		limit, offset int
		want          []string
	}{
		{"first page", 2, 0, []string{"r1", "r2"}},
		{"middle page", 2, 2, []string{"r3", "r4"}},
		{"last incomplete page", 2, 4, []string{"r5"}},
		{"limit bigger than total", 10, 0, []string{"r1", "r2", "r3", "r4", "r5"}},
		{"offset equals total", 10, 5, []string{}},
		{"offset beyond total", 10, 100, []string{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tree, err := s.GetCommentTree(context.Background(), "p1", tc.limit, tc.offset)
			if err != nil {
				t.Fatalf("GetCommentTree() error: %v", err)
			}
			if got := rootIDs(tree); !slices.Equal(got, tc.want) {
				t.Fatalf("roots = %v, want %v", got, tc.want)
			}
		})
	}

	t.Run("replies come with their root", func(t *testing.T) {
		tree, err := s.GetCommentTree(context.Background(), "p1", 1, 2)
		if err != nil {
			t.Fatalf("GetCommentTree() error: %v", err)
		}
		if len(tree) != 1 || tree[0].Comment.ID != "r3" {
			t.Fatalf("roots = %v, want [r3]", rootIDs(tree))
		}
		if got := replyIDs(&tree[0]); !slices.Equal(got, []string{"a1", "a2"}) {
			t.Fatalf("replies = %v, want [a1 a2]", got)
		}
	})
}

func TestMemoryStore_GetCommentTree_OnlyOwnPost(t *testing.T) {
	ctx := context.Background()
	s := newStoreWithPost(t, true)
	if err := s.CreatePost(ctx, domain.Post{ID: "p2", Author: "Alice", CanComment: true}); err != nil {
		t.Fatalf("CreatePost() error: %v", err)
	}
	addComment(t, s, "c1", nil)
	if err := s.CreateComment(ctx, domain.Comment{ID: "c2", PostID: "p2", Message: "other", Author: "Bob"}); err != nil {
		t.Fatalf("CreateComment() error: %v", err)
	}

	tree, err := s.GetCommentTree(ctx, "p1", 10, 0)
	if err != nil {
		t.Fatalf("GetCommentTree() error: %v", err)
	}
	if got := rootIDs(tree); !slices.Equal(got, []string{"c1"}) {
		t.Fatalf("roots = %v, want [c1]", got)
	}
}

func TestMemoryStore_GetCommentTree_MissingParent(t *testing.T) {
	s := newStoreWithPost(t, true)

	// Имитируем повреждённое состояние хранилища: родителя нет.
	s.comments["c1"] = domain.Comment{ID: "c1", PostID: "p1", ParentID: stringPtr("missing")}
	s.commentsByPost["p1"] = []string{"c1"}

	_, err := s.GetCommentTree(context.Background(), "p1", 10, 0)
	if !errors.Is(err, domain.ErrCommentNotFound) {
		t.Fatalf("error = %v, want %v", err, domain.ErrCommentNotFound)
	}
}

func TestMemoryStore_DisableComments(t *testing.T) {
	tests := []struct {
		name         string
		postID       string
		author       string
		wantErr      error
		wantDisabled bool
	}{
		{"author", "p1", "Alice", nil, true},
		{"another user", "p1", "Bob", domain.ErrNotPostAuthor, false},
		{"author name is case sensitive", "p1", "alice", domain.ErrNotPostAuthor, false},
		{"missing post", "missing", "Alice", domain.ErrPostNotFound, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s := newStoreWithPost(t, true)

			got, err := s.DisableComments(ctx, tc.postID, tc.author)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil && got.CanComment {
				t.Fatal("returned post still has CanComment = true")
			}

			saved, err := s.GetPost(ctx, "p1")
			if err != nil {
				t.Fatalf("GetPost() error: %v", err)
			}
			if saved.CanComment == tc.wantDisabled {
				t.Fatalf("stored CanComment = %v, want %v", saved.CanComment, !tc.wantDisabled)
			}
		})
	}
}

func TestMemoryStore_CommentsRejectedAfterDisable(t *testing.T) {
	ctx := context.Background()
	s := newStoreWithPost(t, true)
	addComment(t, s, "c1", nil)

	if _, err := s.DisableComments(ctx, "p1", "Alice"); err != nil {
		t.Fatalf("DisableComments() error: %v", err)
	}

	err := s.CreateComment(ctx, domain.Comment{ID: "c2", PostID: "p1", Message: "late", Author: "Bob"})
	if !errors.Is(err, domain.ErrCommentsForbidden) {
		t.Fatalf("error = %v, want %v", err, domain.ErrCommentsForbidden)
	}

	// Старые комментарии остаются доступными для чтения.
	tree, err := s.GetCommentTree(ctx, "p1", 10, 0)
	if err != nil {
		t.Fatalf("GetCommentTree() error: %v", err)
	}
	if got := rootIDs(tree); !slices.Equal(got, []string{"c1"}) {
		t.Fatalf("roots = %v, want [c1]", got)
	}
}

// Тесты ниже имеет смысл запускать с флагом -race.

func TestMemoryStore_ConcurrentCreateAndRead(t *testing.T) {
	ctx := context.Background()
	s := newStoreWithPost(t, true)

	const n = 100
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			err := s.CreateComment(ctx, domain.Comment{
				ID: fmt.Sprintf("c%d", i), PostID: "p1", Message: "m", Author: "Bob",
			})
			if err != nil {
				t.Errorf("CreateComment() error: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := s.GetCommentTree(ctx, "p1", 10, 0); err != nil {
				t.Errorf("GetCommentTree() error: %v", err)
			}
		}()
	}
	wg.Wait()

	tree, err := s.GetCommentTree(ctx, "p1", n, 0)
	if err != nil {
		t.Fatalf("GetCommentTree() error: %v", err)
	}
	if len(tree) != n {
		t.Fatalf("got %d comments, want %d", len(tree), n)
	}
}

func TestMemoryStore_ConcurrentDisable(t *testing.T) {
	ctx := context.Background()
	s := newStoreWithPost(t, true)

	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.CreateComment(ctx, domain.Comment{
				ID: fmt.Sprintf("c%d", i), PostID: "p1", Message: "m", Author: "Bob",
			})
			if err != nil && !errors.Is(err, domain.ErrCommentsForbidden) {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	if _, err := s.DisableComments(ctx, "p1", "Alice"); err != nil {
		t.Fatalf("DisableComments() error: %v", err)
	}
	wg.Wait()

	err := s.CreateComment(ctx, domain.Comment{ID: "late", PostID: "p1", Message: "m", Author: "Bob"})
	if !errors.Is(err, domain.ErrCommentsForbidden) {
		t.Fatalf("after disable: error = %v, want %v", err, domain.ErrCommentsForbidden)
	}
}
