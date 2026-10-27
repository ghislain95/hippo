package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/oapi-codegen/nullable"

	"github.com/ghislain95/hippo/backend/internal/auth"
	"github.com/ghislain95/hippo/backend/internal/capture"
	"github.com/ghislain95/hippo/backend/internal/store"
)

// itemsPath is the instance of the problems of POST /api/items.
const itemsPath = "/api/items"

// CreateItem captures a link or a note (POST /api/items, E2-01 to E2-03). An image capture
// answers 501 until E2-04.
func (h *handlers) CreateItem(ctx context.Context, request CreateItemRequestObject) (CreateItemResponseObject, error) {
	if request.MultipartBody != nil {
		return nil, errNotImplemented // image capture, E2-04
	}
	if request.JSONBody == nil {
		return CreateItem415ApplicationProblemPlusJSONResponse(newProblem(http.StatusUnsupportedMediaType, problemTypeBlank,
			"Unsupported media type", "Le corps doit être en application/json ou en multipart/form-data.", itemsPath)), nil
	}
	userID, ok := auth.UserID(ctx)
	if !ok {
		return nil, errors.New("create item: no authenticated user")
	}
	body := request.JSONBody
	var source *string
	if body.Source != nil {
		value := string(*body.Source)
		source = &value
	}
	result, err := h.capture.Capture(ctx, userID, capture.Request{
		URL:       body.Url,
		Note:      body.Note,
		UserNote:  body.UserNote,
		IsPrivate: body.IsPrivate,
		Source:    source,
		ClientID:  body.ClientId,
	})
	if err != nil {
		return createItemError(err)
	}
	if result.Created {
		return CreateItem201JSONResponse(itemFromStore(result.Item)), nil
	}
	return CreateItem200JSONResponse(itemFromStore(result.Item)), nil
}

// createItemError translates a refused capture into its problem; any other error is a 500.
func createItemError(err error) (CreateItemResponseObject, error) {
	badRequest := func(problemType, title, detail string) CreateItemResponseObject {
		return CreateItem400ApplicationProblemPlusJSONResponse{
			ProblemApplicationProblemPlusJSONResponse(newProblem(http.StatusBadRequest, problemType, title, detail, itemsPath)),
		}
	}
	var validation *capture.ValidationError
	switch {
	case errors.Is(err, capture.ErrEmptyCapture):
		return badRequest(problemTypeEmptyCapture, "Empty capture", "La capture ne contient ni lien ni note."), nil
	case errors.Is(err, capture.ErrEmptyNote):
		return badRequest(problemTypeEmptyNote, "Empty note", "La note est vide."), nil
	case errors.Is(err, capture.ErrAmbiguousCapture):
		return badRequest(problemTypeAmbiguousCapture, "Ambiguous capture",
			"La capture contient à la fois un lien et une note, ou une note et une note de capture."), nil
	case errors.Is(err, capture.ErrInvalidURL):
		return badRequest(problemTypeInvalidURL, "Invalid URL", "Le lien doit être une adresse http ou https valide."), nil
	case errors.As(err, &validation):
		return badRequest(problemTypeBlank, "Bad request", "La requête est invalide."), nil
	default:
		return nil, fmt.Errorf("create item: %w", err)
	}
}

// itemFromStore maps a stored item to the Item of the contract. Every nullable member is set
// explicitly: an unset nullable.Nullable would encode as its zero value, not as null. Arrays
// are empty, never null. Content type, category, tags, fiche and assets have no column yet.
func itemFromStore(item store.Item) Item {
	userEdited := make([]CorrectedField, 0, len(item.UserEdited))
	for _, field := range item.UserEdited {
		userEdited = append(userEdited, CorrectedField(field))
	}
	return Item{
		Id:               item.ID,
		Kind:             ItemKind(item.Kind),
		Source:           CaptureSource(item.Source),
		Title:            item.Title,
		Url:              nullableOf(item.URL),
		SiteName:         nullableOf(item.SiteName),
		UserNote:         nullableOf(item.UserNote),
		IsPrivate:        item.IsPrivate,
		Status:           ProcessingStatus(item.Status),
		ExtractionMode:   nullableOf(enumOf[ExtractionMode](item.ExtractionMode)),
		ContentTruncated: item.ContentTruncated,
		ReadingMinutes:   nullableOf(intOf(item.ReadingMinutes)),
		ContentType:      nullable.NewNullNullable[ContentTypeKey](),
		CategoryId:       nullable.NewNullNullable[int64](),
		TagIds:           []int64{},
		ContentOk:        nullableOf(item.ContentOK),
		ContentIssue:     nullableOf(item.ContentIssue),
		Language:         nullableOf(item.Language),
		Freshness:        nullableOf(enumOf[Freshness](item.Freshness)),
		Model:            nullableOf(item.Model),
		PromptVersion:    nullableOf(intOf(item.PromptVersion)),
		SchemaVersion:    nullableOf(intOf(item.SchemaVersion)),
		UserEdited:       userEdited,
		Annotations:      nullableOf(item.Annotations),
		IsPinned:         item.IsPinned,
		ReadStatus:       ReadStatus(item.ReadStatus),
		ReadProgress:     float32(item.ReadProgress),
		Attempts:         int(item.Attempts),
		LastError:        nullableOf(item.LastError),
		Assets:           []Asset{},
		CreatedAt:        item.CreatedAt,
		UpdatedAt:        item.UpdatedAt,
		ExtractedAt:      nullableOf(item.ExtractedAt),
		AnalyzedAt:       nullableOf(item.AnalyzedAt),
		ReadAt:           nullableOf(item.ReadAt),
	}
}

// nullableOf is null for a nil pointer, the value otherwise.
func nullableOf[T any](value *T) nullable.Nullable[T] {
	if value == nil {
		return nullable.NewNullNullable[T]()
	}
	return nullable.NewNullableWithValue(*value)
}

func enumOf[E ~string](value *string) *E {
	if value == nil {
		return nil
	}
	converted := E(*value)
	return &converted
}

func intOf(value *int64) *int {
	if value == nil {
		return nil
	}
	converted := int(*value)
	return &converted
}
