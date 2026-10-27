package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"

	"github.com/ghislain95/hippo/backend/internal/auth"
	"github.com/ghislain95/hippo/backend/internal/store"
)

// assetCacheControl lets the app keep an image for good: an asset never changes (ADR-0018).
const assetCacheControl = "private, max-age=31536000, immutable"

// GetAsset streams the image file of an asset to its owner, unchanged (GET
// /api/assets/{asset_id}, E2-04). An unknown asset or another user's answers 404 (RG-90). The
// path parameter only selects the row: the file path comes from the row, opened inside the
// assets directory.
func (h *handlers) GetAsset(ctx context.Context, request GetAssetRequestObject) (GetAssetResponseObject, error) {
	userID, ok := auth.UserID(ctx)
	if !ok {
		return nil, errors.New("get asset: no authenticated user")
	}
	asset, err := h.assets.Asset(ctx, userID, request.AssetId)
	if errors.Is(err, store.ErrNotFound) {
		return GetAsset404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse(newProblem(
			http.StatusNotFound, problemTypeBlank, "Not found", "Cette image n'existe pas.", "/api/assets/"+request.AssetId))}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get asset %s: %w", request.AssetId, err)
	}
	file, err := h.assetFiles.Open(asset.Path)
	if err != nil {
		return nil, fmt.Errorf("get asset %s: open file: %w", asset.ID, withoutPath(err))
	}
	info, err := file.Stat()
	if err != nil {
		closeErr := file.Close()
		return nil, errors.Join(fmt.Errorf("get asset %s: stat file: %w", asset.ID, withoutPath(err)), closeErr)
	}
	cacheControl, noSniffValue := assetCacheControl, "nosniff"
	return GetAsset200ImageResponse{
		Body:          file, // closed by the generated response once copied
		ContentType:   asset.MimeType,
		ContentLength: info.Size(),
		Headers:       GetAsset200ResponseHeaders{CacheControl: &cacheControl, XContentTypeOptions: &noSniffValue},
	}, nil
}

// withoutPath drops the path of a file system error, so that the log of a failed download
// names the asset id only.
func withoutPath(err error) error {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err
	}
	return err
}
