package graph

import (
	"comment_service/graph/model"
	"comment_service/internal/domain"
)

func commentTreeToModel(tree *domain.CommentTree) *model.Comment {
	if tree == nil {
		return nil
	}

	comment := tree.Comment

	result := &model.Comment{
		ID:       comment.ID,
		PostID:   comment.PostID,
		ParentID: comment.ParentID,
		Message:  comment.Message,
		Author:   comment.Author,
		Replies:  make([]*model.Comment, 0, len(tree.Replies)),
	}

	for _, reply := range tree.Replies {
		result.Replies = append(result.Replies, commentTreeToModel(reply))
	}

	return result
}
