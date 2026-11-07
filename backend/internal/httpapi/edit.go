package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/ghislain95/hippo/backend/internal/auth"
	"github.com/ghislain95/hippo/backend/internal/store"
)

// UpdateItem changes an item of the authenticated user, PATCH /api/items/{item_id}. Only the
// privacy flag is implemented (E2-07): a body with any other member of ItemPatch answers 501
// before anything is read or written, so that a mixed request applies nothing, until E4-03
// implements the corrections. The flag never changes the processing status (RG-14). Another
// user's item or an unknown one answers 404 (RG-90).
func (h *handlers) UpdateItem(ctx context.Context, request UpdateItemRequestObject) (UpdateItemResponseObject, error) {
	instance := itemsPath + "/" + request.ItemId
	badRequest := UpdateItem400ApplicationProblemPlusJSONResponse{BadRequestApplicationProblemPlusJSONResponse(newProblem(
		http.StatusBadRequest, problemTypeBlank, "Bad request", "La requête est invalide.", instance))}
	patch := request.Body
	if patch == nil {
		return badRequest, nil
	}
	if patch.ContentType != nil || patch.CategoryId.IsSpecified() || patch.Tags != nil || patch.IsPinned != nil ||
		patch.Annotations.IsSpecified() || patch.UserNote.IsSpecified() {
		return nil, errNotImplemented
	}
	// {} (minProperties: 1); a null is_private is refused before, by strictJSONBodies.
	if patch.IsPrivate == nil {
		return badRequest, nil
	}
	userID, ok := auth.UserID(ctx)
	if !ok {
		return nil, errors.New("update item: no authenticated user")
	}
	item, err := h.items.SetPrivate(ctx, userID, request.ItemId, *patch.IsPrivate)
	if errors.Is(err, store.ErrNotFound) {
		return UpdateItem404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse(newProblem(
			http.StatusNotFound, problemTypeBlank, "Not found", "Cet élément n'existe pas.", instance))}, nil
	}
	if err != nil {
		// No item id here: the path segment is the client's, not checked as a ULID.
		return nil, fmt.Errorf("update item: %w", err)
	}
	answer, err := itemFromStore(item)
	if err != nil {
		return nil, fmt.Errorf("update item: %w", err)
	}
	return UpdateItem200JSONResponse(answer), nil
}
