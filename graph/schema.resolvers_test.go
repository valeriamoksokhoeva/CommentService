package graph

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"comment_service/graph/model"
	"comment_service/internal/domain"
	"comment_service/internal/store"
	"comment_service/internal/subscription"
)

// Тесты резолверов работают поверх in-memory хранилища: они проверяют,
// как резолверы связывают входные данные, хранилище и подписки.

const testPostID = "post-1"

func newTestResolver() (*Resolver, *store.MemoryStore) {
	memoryStore := store.NewMemoryStore()
	resolver := &Resolver{
		Store:             memoryStore,
		SubscriberManager: subscription.NewSubsctiptionManager(),
	}
	return resolver, memoryStore
}

// seedPost кладёт пост post-1 от Alice прямо в хранилище.
func seedPost(t *testing.T, s store.Repository, canComment bool) {
	t.Helper()

	err := s.CreatePost(context.Background(), domain.Post{
		ID:         testPostID,
		Message:    "Test post",
		Author:     "Alice",
		CanComment: canComment,
	})
	if err != nil {
		t.Fatalf("CreatePost() error: %v", err)
	}
}

// addComment создаёт комментарий через резолвер и падает, если это не удалось.
func addComment(t *testing.T, r *Resolver, parentID *string, message string) *model.Comment {
	t.Helper()

	got, err := r.Mutation().CreateComment(context.Background(), model.CreateCommentInput{
		PostID:   testPostID,
		ParentID: parentID,
		Message:  message,
		Author:   "Bob",
	})
	if err != nil {
		t.Fatalf("CreateComment(%q) error: %v", message, err)
	}
	return got
}

func commentIDs(comments []*model.Comment) []string {
	ids := make([]string, 0, len(comments))
	for _, c := range comments {
		ids = append(ids, c.ID)
	}
	return ids
}

// ---------- Posts ----------

func TestCreatePost(t *testing.T) {
	ctx := context.Background()
	r, memoryStore := newTestResolver()

	got, err := r.Mutation().CreatePost(ctx, model.CreatePostInput{
		Message:    "Hello, GraphQL!",
		Author:     "Alice",
		CanComment: true,
	})
	if err != nil {
		t.Fatalf("CreatePost() error: %v", err)
	}
	if got.ID == "" {
		t.Error("CreatePost() returned an empty ID")
	}
	if got.Message != "Hello, GraphQL!" || got.Author != "Alice" || !got.CanComment {
		t.Errorf("CreatePost() = %+v, fields do not match the input", got)
	}

	saved, err := memoryStore.GetPost(ctx, got.ID)
	if err != nil {
		t.Fatalf("GetPost() error: %v", err)
	}
	if saved.Message != got.Message || saved.Author != got.Author || saved.CanComment != got.CanComment {
		t.Errorf("stored post = %+v, want fields of %+v", saved, got)
	}
}

func TestCreatePost_Validation(t *testing.T) {
	tests := []struct {
		name  string
		input model.CreatePostInput
		want  error
	}{
		{"empty message", model.CreatePostInput{Message: "", Author: "Alice"}, domain.ErrEmptyMessage},
		{"whitespace message", model.CreatePostInput{Message: " \t\n", Author: "Alice"}, domain.ErrEmptyMessage},
		{"empty author", model.CreatePostInput{Message: "text", Author: ""}, domain.ErrEmptyAuthor},
		{"whitespace author", model.CreatePostInput{Message: "text", Author: "   "}, domain.ErrEmptyAuthor},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			r, _ := newTestResolver()

			if _, err := r.Mutation().CreatePost(ctx, tc.input); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}

			posts, err := r.Query().Posts(ctx)
			if err != nil {
				t.Fatalf("Posts() error: %v", err)
			}
			if len(posts) != 0 {
				t.Fatalf("invalid post was saved: %d posts in the store", len(posts))
			}
		})
	}
}

func TestPost(t *testing.T) {
	ctx := context.Background()
	r, memoryStore := newTestResolver()
	seedPost(t, memoryStore, true)

	t.Run("found", func(t *testing.T) {
		got, err := r.Query().Post(ctx, testPostID)
		if err != nil {
			t.Fatalf("Post() error: %v", err)
		}
		if got.ID != testPostID || got.Message != "Test post" || got.Author != "Alice" || !got.CanComment {
			t.Errorf("Post() = %+v, want the seeded post", got)
		}
	})

	t.Run("not found", func(t *testing.T) {
		if _, err := r.Query().Post(ctx, "missing"); !errors.Is(err, domain.ErrPostNotFound) {
			t.Fatalf("error = %v, want %v", err, domain.ErrPostNotFound)
		}
	})
}

func TestPosts(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestResolver()

	empty, err := r.Query().Posts(ctx)
	if err != nil {
		t.Fatalf("Posts() error: %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("Posts() on empty store = %#v, want empty non-nil slice", empty)
	}

	for _, message := range []string{"first", "second"} {
		_, err := r.Mutation().CreatePost(ctx, model.CreatePostInput{Message: message, Author: "Alice", CanComment: true})
		if err != nil {
			t.Fatalf("CreatePost() error: %v", err)
		}
	}

	posts, err := r.Query().Posts(ctx)
	if err != nil {
		t.Fatalf("Posts() error: %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("got %d posts, want 2", len(posts))
	}
}

func TestDisableComments(t *testing.T) {
	ctx := context.Background()

	t.Run("author disables comments", func(t *testing.T) {
		r, memoryStore := newTestResolver()
		seedPost(t, memoryStore, true)

		got, err := r.Mutation().DisableComments(ctx, testPostID, "Alice")
		if err != nil {
			t.Fatalf("DisableComments() error: %v", err)
		}
		if got.CanComment {
			t.Error("returned post has CanComment = true")
		}

		saved, err := memoryStore.GetPost(ctx, testPostID)
		if err != nil {
			t.Fatalf("GetPost() error: %v", err)
		}
		if saved.CanComment {
			t.Error("stored post has CanComment = true")
		}

		_, err = r.Mutation().CreateComment(ctx, model.CreateCommentInput{
			PostID: testPostID, Message: "late", Author: "Bob",
		})
		if !errors.Is(err, domain.ErrCommentsForbidden) {
			t.Fatalf("CreateComment() after disable: error = %v, want %v", err, domain.ErrCommentsForbidden)
		}
	})

	t.Run("not the author", func(t *testing.T) {
		r, memoryStore := newTestResolver()
		seedPost(t, memoryStore, true)

		_, err := r.Mutation().DisableComments(ctx, testPostID, "Mallory")
		if !errors.Is(err, domain.ErrNotPostAuthor) {
			t.Fatalf("error = %v, want %v", err, domain.ErrNotPostAuthor)
		}

		saved, err := memoryStore.GetPost(ctx, testPostID)
		if err != nil {
			t.Fatalf("GetPost() error: %v", err)
		}
		if !saved.CanComment {
			t.Error("comments were disabled by a non-author")
		}
	})

	t.Run("post not found", func(t *testing.T) {
		r, _ := newTestResolver()

		_, err := r.Mutation().DisableComments(ctx, "missing", "Alice")
		if !errors.Is(err, domain.ErrPostNotFound) {
			t.Fatalf("error = %v, want %v", err, domain.ErrPostNotFound)
		}
	})
}

// ---------- Comments ----------

func TestCreateComment(t *testing.T) {
	r, memoryStore := newTestResolver()
	seedPost(t, memoryStore, true)

	got := addComment(t, r, nil, "First comment")

	if got.ID == "" {
		t.Error("comment ID is empty")
	}
	if got.PostID != testPostID || got.Message != "First comment" || got.Author != "Bob" {
		t.Errorf("CreateComment() = %+v, fields do not match the input", got)
	}
	if got.ParentID != nil {
		t.Errorf("ParentID = %q, want nil", *got.ParentID)
	}
	if got.Replies == nil || len(got.Replies) != 0 {
		t.Errorf("Replies = %v, want empty non-nil slice", got.Replies)
	}
}

func TestCreateComment_Reply(t *testing.T) {
	r, memoryStore := newTestResolver()
	seedPost(t, memoryStore, true)

	parent := addComment(t, r, nil, "Parent")
	got := addComment(t, r, &parent.ID, "Reply")

	if got.ParentID == nil || *got.ParentID != parent.ID {
		t.Fatalf("ParentID = %v, want %q", got.ParentID, parent.ID)
	}
}

func TestCreateComment_EmptyParentIDMeansRoot(t *testing.T) {
	ctx := context.Background()
	r, memoryStore := newTestResolver()
	seedPost(t, memoryStore, true)

	empty := ""
	got := addComment(t, r, &empty, "root")
	if got.ParentID != nil {
		t.Fatalf("ParentID = %q, want nil", *got.ParentID)
	}

	tree, err := r.Query().Comments(ctx, testPostID, 10, 0)
	if err != nil {
		t.Fatalf("Comments() error: %v", err)
	}
	if got := commentIDs(tree); len(got) != 1 {
		t.Fatalf("roots = %v, want exactly the new comment", got)
	}
}

func TestCreateComment_Errors(t *testing.T) {
	tests := []struct {
		name     string
		disabled bool
		input    model.CreateCommentInput
		want     error
	}{
		{
			name:  "post not found",
			input: model.CreateCommentInput{PostID: "missing", Message: "hi", Author: "Bob"},
			want:  domain.ErrPostNotFound,
		},
		{
			name:     "comments disabled",
			disabled: true,
			input:    model.CreateCommentInput{PostID: testPostID, Message: "hi", Author: "Bob"},
			want:     domain.ErrCommentsForbidden,
		},
		{
			name:  "empty message",
			input: model.CreateCommentInput{PostID: testPostID, Message: "", Author: "Bob"},
			want:  domain.ErrEmptyMessage,
		},
		{
			name:  "whitespace message",
			input: model.CreateCommentInput{PostID: testPostID, Message: "  \n", Author: "Bob"},
			want:  domain.ErrEmptyMessage,
		},
		{
			name:  "empty author",
			input: model.CreateCommentInput{PostID: testPostID, Message: "hi", Author: ""},
			want:  domain.ErrEmptyAuthor,
		},
		{
			name:  "message over 2000 characters",
			input: model.CreateCommentInput{PostID: testPostID, Message: strings.Repeat("я", 2001), Author: "Bob"},
			want:  domain.ErrCommentTooLong,
		},
		{
			name: "parent not found",
			input: model.CreateCommentInput{
				PostID: testPostID, ParentID: strPtr("missing"), Message: "hi", Author: "Bob",
			},
			want: domain.ErrCommentNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, memoryStore := newTestResolver()
			seedPost(t, memoryStore, !tc.disabled)

			_, err := r.Mutation().CreateComment(context.Background(), tc.input)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestCreateComment_MaxLengthAccepted(t *testing.T) {
	r, memoryStore := newTestResolver()
	seedPost(t, memoryStore, true)

	addComment(t, r, nil, strings.Repeat("я", 2000))
}

func TestCreateComment_ParentFromAnotherPost(t *testing.T) {
	ctx := context.Background()
	r, memoryStore := newTestResolver()
	seedPost(t, memoryStore, true)
	if err := memoryStore.CreatePost(ctx, domain.Post{ID: "post-2", Author: "Alice", CanComment: true}); err != nil {
		t.Fatalf("CreatePost() error: %v", err)
	}
	parent := addComment(t, r, nil, "root of post-1")

	_, err := r.Mutation().CreateComment(ctx, model.CreateCommentInput{
		PostID: "post-2", ParentID: &parent.ID, Message: "reply", Author: "Bob",
	})
	if !errors.Is(err, domain.ErrCommentsNoMatch) {
		t.Fatalf("error = %v, want %v", err, domain.ErrCommentsNoMatch)
	}
}

func TestComments_NestedTree(t *testing.T) {
	r, memoryStore := newTestResolver()
	seedPost(t, memoryStore, true)

	root := addComment(t, r, nil, "Root")
	child := addComment(t, r, &root.ID, "Reply")
	addComment(t, r, &child.ID, "Nested reply")

	got, err := r.Query().Comments(context.Background(), testPostID, 10, 0)
	if err != nil {
		t.Fatalf("Comments() error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d roots, want 1", len(got))
	}
	if len(got[0].Replies) != 1 {
		t.Fatalf("root has %d replies, want 1", len(got[0].Replies))
	}
	if len(got[0].Replies[0].Replies) != 1 {
		t.Fatalf("reply has %d replies, want 1", len(got[0].Replies[0].Replies))
	}
	if msg := got[0].Replies[0].Replies[0].Message; msg != "Nested reply" {
		t.Errorf("nested message = %q, want %q", msg, "Nested reply")
	}
}

func TestComments_Pagination(t *testing.T) {
	ctx := context.Background()
	r, memoryStore := newTestResolver()
	seedPost(t, memoryStore, true)

	ids := make([]string, 3)
	for i, message := range []string{"one", "two", "three"} {
		ids[i] = addComment(t, r, nil, message).ID
	}
	// Ответ не должен занимать место на странице корневых комментариев.
	addComment(t, r, &ids[1], "reply")

	tests := []struct {
		name          string
		limit, offset int32
		want          []string
	}{
		{"first page", 2, 0, ids[0:2]},
		{"second page", 2, 2, ids[2:3]},
		{"single item", 1, 1, ids[1:2]},
		{"offset beyond total", 10, 10, []string{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := r.Query().Comments(ctx, testPostID, tc.limit, tc.offset)
			if err != nil {
				t.Fatalf("Comments() error: %v", err)
			}
			ids := commentIDs(got)
			if len(ids) != len(tc.want) {
				t.Fatalf("roots = %v, want %v", ids, tc.want)
			}
			for i := range ids {
				if ids[i] != tc.want[i] {
					t.Fatalf("roots = %v, want %v", ids, tc.want)
				}
			}
		})
	}
}

func TestComments_InvalidPagination(t *testing.T) {
	r, memoryStore := newTestResolver()
	seedPost(t, memoryStore, true)

	tests := []struct {
		name          string
		limit, offset int32
	}{
		{"zero limit", 0, 0},
		{"negative limit", -1, 0},
		{"limit exceeds maximum", 101, 0},
		{"negative offset", 10, -1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := r.Query().Comments(
				context.Background(),
				testPostID,
				tc.limit,
				tc.offset,
			)

			if !errors.Is(err, domain.ErrInvalidPagination) {
				t.Fatalf(
					"Comments() error = %v, want %v",
					err,
					domain.ErrInvalidPagination,
				)
			}
		})
	}
}

func TestComments_PostNotFound(t *testing.T) {
	r, _ := newTestResolver()

	_, err := r.Query().Comments(context.Background(), "missing", 10, 0)
	if !errors.Is(err, domain.ErrPostNotFound) {
		t.Fatalf("error = %v, want %v", err, domain.ErrPostNotFound)
	}
}

func TestComments_NoComments(t *testing.T) {
	r, memoryStore := newTestResolver()
	seedPost(t, memoryStore, true)

	got, err := r.Query().Comments(context.Background(), testPostID, 10, 0)
	if err != nil {
		t.Fatalf("Comments() error: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("Comments() = %#v, want empty non-nil slice", got)
	}
}

// ---------- Subscriptions ----------

func receiveComment(t *testing.T, ch <-chan *model.Comment) *model.Comment {
	t.Helper()

	select {
	case c, ok := <-ch:
		if !ok {
			t.Fatal("subscription channel is closed")
		}
		return c
	case <-time.After(time.Second):
		t.Fatal("no comment received from subscription")
	}
	return nil
}

func expectNothing(t *testing.T, ch <-chan *model.Comment) {
	t.Helper()

	select {
	case c, ok := <-ch:
		t.Fatalf("unexpected receive from subscription: comment=%+v, open=%v", c, ok)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestCommentAdded_DeliversNewComment(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, memoryStore := newTestResolver()
	seedPost(t, memoryStore, true)

	ch, err := r.Subscription().CommentAdded(ctx, testPostID)
	if err != nil {
		t.Fatalf("CommentAdded() error: %v", err)
	}

	root := addComment(t, r, nil, "root")
	reply := addComment(t, r, &root.ID, "reply")

	got := receiveComment(t, ch)
	if got.ID != root.ID || got.Message != "root" || got.Author != "Bob" || got.PostID != testPostID {
		t.Errorf("first comment = %+v, want %+v", got, root)
	}
	got = receiveComment(t, ch)
	if got.ID != reply.ID || got.ParentID == nil || *got.ParentID != root.ID {
		t.Errorf("second comment = %+v, want reply to %s", got, root.ID)
	}
}

func TestCommentAdded_AllSubscribersReceive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, memoryStore := newTestResolver()
	seedPost(t, memoryStore, true)

	first, err := r.Subscription().CommentAdded(ctx, testPostID)
	if err != nil {
		t.Fatalf("CommentAdded() error: %v", err)
	}
	second, err := r.Subscription().CommentAdded(ctx, testPostID)
	if err != nil {
		t.Fatalf("CommentAdded() error: %v", err)
	}

	created := addComment(t, r, nil, "hello")

	for name, ch := range map[string]<-chan *model.Comment{"first": first, "second": second} {
		if got := receiveComment(t, ch); got.ID != created.ID {
			t.Errorf("%s subscriber got %q, want %q", name, got.ID, created.ID)
		}
	}
}

func TestCommentAdded_OtherPostIsIgnored(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, memoryStore := newTestResolver()
	seedPost(t, memoryStore, true)
	if err := memoryStore.CreatePost(ctx, domain.Post{ID: "post-2", Author: "Alice", CanComment: true}); err != nil {
		t.Fatalf("CreatePost() error: %v", err)
	}

	ch, err := r.Subscription().CommentAdded(ctx, "post-2")
	if err != nil {
		t.Fatalf("CommentAdded() error: %v", err)
	}

	addComment(t, r, nil, "comment for post-1")

	expectNothing(t, ch)
}

func TestCommentAdded_FailedCommentIsNotPublished(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, memoryStore := newTestResolver()
	seedPost(t, memoryStore, false)

	ch, err := r.Subscription().CommentAdded(ctx, testPostID)
	if err != nil {
		t.Fatalf("CommentAdded() error: %v", err)
	}

	_, err = r.Mutation().CreateComment(ctx, model.CreateCommentInput{
		PostID: testPostID, Message: "rejected", Author: "Bob",
	})
	if !errors.Is(err, domain.ErrCommentsForbidden) {
		t.Fatalf("CreateComment() error = %v, want %v", err, domain.ErrCommentsForbidden)
	}

	expectNothing(t, ch)
}

func TestCommentAdded_PostNotFound(t *testing.T) {
	r, _ := newTestResolver()

	_, err := r.Subscription().CommentAdded(context.Background(), "missing")
	if !errors.Is(err, domain.ErrPostNotFound) {
		t.Fatalf("error = %v, want %v", err, domain.ErrPostNotFound)
	}
}

func TestCommentAdded_ChannelClosesOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r, memoryStore := newTestResolver()
	seedPost(t, memoryStore, true)

	ch, err := r.Subscription().CommentAdded(ctx, testPostID)
	if err != nil {
		t.Fatalf("CommentAdded() error: %v", err)
	}

	cancel()

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("channel delivered a value instead of closing")
		}
	case <-time.After(time.Second):
		t.Fatal("channel was not closed after context cancel")
	}

	// После отмены подписки создание комментариев не должно зависать.
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := r.Mutation().CreateComment(context.Background(), model.CreateCommentInput{
			PostID: testPostID, Message: "after cancel", Author: "Bob",
		})
		if err != nil {
			t.Errorf("CreateComment() error: %v", err)
		}
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("CreateComment() blocked after the subscriber left")
	}
}

func strPtr(v string) *string {
	return &v
}
