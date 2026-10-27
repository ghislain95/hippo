package httpapi

import (
	"context"
	"errors"
)

// errNotImplemented is returned by the operations of the contract that no story has implemented
// yet; the server answers 501 with the problem type urn:hippo:problem:not-implemented
// (api-conventions spec, design D7 of api-contract-v1).
var errNotImplemented = errors.New("operation not implemented yet")

// notImplemented implements every operation of StrictServerInterface with errNotImplemented.
// handlers embeds it: a story implements an operation as a method of handlers, in its own file,
// which shadows the stub, and deletes the stub below in the same change. An operation implemented
// in part (a request format, a field) returns errNotImplemented for the rest.
type notImplemented struct{}

// GetAsset is not implemented yet.
func (notImplemented) GetAsset(context.Context, GetAssetRequestObject) (GetAssetResponseObject, error) {
	return nil, errNotImplemented
}

// ListCategories is not implemented yet.
func (notImplemented) ListCategories(context.Context, ListCategoriesRequestObject) (ListCategoriesResponseObject, error) {
	return nil, errNotImplemented
}

// CreateCategory is not implemented yet.
func (notImplemented) CreateCategory(context.Context, CreateCategoryRequestObject) (CreateCategoryResponseObject, error) {
	return nil, errNotImplemented
}

// DeleteCategory is not implemented yet.
func (notImplemented) DeleteCategory(context.Context, DeleteCategoryRequestObject) (DeleteCategoryResponseObject, error) {
	return nil, errNotImplemented
}

// UpdateCategory is not implemented yet.
func (notImplemented) UpdateCategory(context.Context, UpdateCategoryRequestObject) (UpdateCategoryResponseObject, error) {
	return nil, errNotImplemented
}

// MergeCategory is not implemented yet.
func (notImplemented) MergeCategory(context.Context, MergeCategoryRequestObject) (MergeCategoryResponseObject, error) {
	return nil, errNotImplemented
}

// ListContentTypes is not implemented yet.
func (notImplemented) ListContentTypes(context.Context, ListContentTypesRequestObject) (ListContentTypesResponseObject, error) {
	return nil, errNotImplemented
}

// UpdateContentType is not implemented yet.
func (notImplemented) UpdateContentType(context.Context, UpdateContentTypeRequestObject) (UpdateContentTypeResponseObject, error) {
	return nil, errNotImplemented
}

// MergeContentType is not implemented yet.
func (notImplemented) MergeContentType(context.Context, MergeContentTypeRequestObject) (MergeContentTypeResponseObject, error) {
	return nil, errNotImplemented
}

// ExportArchive is not implemented yet.
func (notImplemented) ExportArchive(context.Context, ExportArchiveRequestObject) (ExportArchiveResponseObject, error) {
	return nil, errNotImplemented
}

// ListItems is not implemented yet.
func (notImplemented) ListItems(context.Context, ListItemsRequestObject) (ListItemsResponseObject, error) {
	return nil, errNotImplemented
}

// DeleteItem is not implemented yet.
func (notImplemented) DeleteItem(context.Context, DeleteItemRequestObject) (DeleteItemResponseObject, error) {
	return nil, errNotImplemented
}

// GetItem is not implemented yet.
func (notImplemented) GetItem(context.Context, GetItemRequestObject) (GetItemResponseObject, error) {
	return nil, errNotImplemented
}

// UpdateItem is not implemented yet.
func (notImplemented) UpdateItem(context.Context, UpdateItemRequestObject) (UpdateItemResponseObject, error) {
	return nil, errNotImplemented
}

// AnalyzeItem is not implemented yet.
func (notImplemented) AnalyzeItem(context.Context, AnalyzeItemRequestObject) (AnalyzeItemResponseObject, error) {
	return nil, errNotImplemented
}

// UpdateReadingProgress is not implemented yet.
func (notImplemented) UpdateReadingProgress(context.Context, UpdateReadingProgressRequestObject) (UpdateReadingProgressResponseObject, error) {
	return nil, errNotImplemented
}

// RetryItem is not implemented yet.
func (notImplemented) RetryItem(context.Context, RetryItemRequestObject) (RetryItemResponseObject, error) {
	return nil, errNotImplemented
}

// GetReview is not implemented yet.
func (notImplemented) GetReview(context.Context, GetReviewRequestObject) (GetReviewResponseObject, error) {
	return nil, errNotImplemented
}

// CompleteReview is not implemented yet.
func (notImplemented) CompleteReview(context.Context, CompleteReviewRequestObject) (CompleteReviewResponseObject, error) {
	return nil, errNotImplemented
}

// GetStats is not implemented yet.
func (notImplemented) GetStats(context.Context, GetStatsRequestObject) (GetStatsResponseObject, error) {
	return nil, errNotImplemented
}

// SyncChanges is not implemented yet.
func (notImplemented) SyncChanges(context.Context, SyncChangesRequestObject) (SyncChangesResponseObject, error) {
	return nil, errNotImplemented
}

// ListTags is not implemented yet.
func (notImplemented) ListTags(context.Context, ListTagsRequestObject) (ListTagsResponseObject, error) {
	return nil, errNotImplemented
}

// UpdateTag is not implemented yet.
func (notImplemented) UpdateTag(context.Context, UpdateTagRequestObject) (UpdateTagResponseObject, error) {
	return nil, errNotImplemented
}

// MergeTag is not implemented yet.
func (notImplemented) MergeTag(context.Context, MergeTagRequestObject) (MergeTagResponseObject, error) {
	return nil, errNotImplemented
}
