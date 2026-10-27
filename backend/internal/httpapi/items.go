package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"mime/multipart"
	"net/http"

	"github.com/oapi-codegen/nullable"

	"github.com/ghislain95/hippo/backend/internal/auth"
	"github.com/ghislain95/hippo/backend/internal/capture"
	"github.com/ghislain95/hippo/backend/internal/store"
)

// itemsPath is the instance of the problems of POST /api/items.
const itemsPath = "/api/items"

// CreateItem captures a link or a note (JSON body) or an image (multipart body), POST
// /api/items (E2-01 to E2-04).
func (h *handlers) CreateItem(ctx context.Context, request CreateItemRequestObject) (CreateItemResponseObject, error) {
	if request.MultipartBody != nil {
		return h.createImageItem(ctx, request.MultipartBody)
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
	return createItemResult(result), nil
}

func createItemResult(result capture.Result) CreateItemResponseObject {
	if result.Created {
		return CreateItem201JSONResponse(itemFromStore(result.Item))
	}
	return CreateItem200JSONResponse(itemFromStore(result.Item))
}

// createImageItem reads a multipart image capture and hands it to the domain. Until then, the
// uploaded file is removed here on every path; once handed over, the domain owns it (kept for
// a new asset, removed otherwise).
func (h *handlers) createImageItem(ctx context.Context, reader *multipart.Reader) (CreateItemResponseObject, error) {
	userID, ok := auth.UserID(ctx)
	if !ok {
		return nil, errors.New("create item: no authenticated user")
	}
	form, err := readImageForm(reader, h.assetFiles)
	handedOver := false
	defer func() {
		if form.image == nil || handedOver {
			return
		}
		// assetfs errors carry no file name.
		if err := form.image.Remove(); err != nil {
			h.logger.ErrorContext(ctx, "remove uploaded image", slog.String("error", err.Error()))
		}
	}()
	var refused *formError
	switch {
	case errors.As(err, &refused):
		return h.formRefusal(ctx, refused), nil
	case err != nil:
		return nil, fmt.Errorf("create item: %w", err)
	}

	var isPrivate *bool
	if raw, present := form.fields["is_private"]; present {
		// Exactly true or false: strconv.ParseBool would accept 1, t or TRUE.
		switch raw {
		case "true", "false":
			value := raw == "true"
			isPrivate = &value
		default:
			return h.formRefusal(ctx, &formError{fault: faultStructure}), nil
		}
	}
	field := func(name string) *string {
		if value, present := form.fields[name]; present {
			return &value
		}
		return nil
	}
	handedOver = true
	result, err := h.capture.CaptureImage(ctx, userID, capture.ImageRequest{
		Image:     form.image,
		UserNote:  field("user_note"),
		IsPrivate: isPrivate,
		Source:    field("source"),
		ClientID:  field("client_id"),
	})
	if err != nil {
		return createItemError(err)
	}
	return createItemResult(result), nil
}

// formRefusal is the problem of a refused multipart body. A read failure is logged by its
// kind only: its error may quote a part.
func (h *handlers) formRefusal(ctx context.Context, refused *formError) CreateItemResponseObject {
	if refused.kind != "" {
		h.logger.InfoContext(ctx, "multipart capture body refused", slog.String("kind", refused.kind))
	}
	switch refused.fault {
	case faultBodyTooLarge:
		return CreateItem413ApplicationProblemPlusJSONResponse(newProblem(http.StatusRequestEntityTooLarge, problemTypeBlank,
			"Payload too large", multipartTooLargeDetail, itemsPath))
	case faultImageTooLarge:
		return imageTooLarge()
	default:
		return CreateItem400ApplicationProblemPlusJSONResponse{
			ProblemApplicationProblemPlusJSONResponse(newProblem(http.StatusBadRequest, problemTypeBlank, "Bad request", "La requête est invalide.", itemsPath)),
		}
	}
}

func imageTooLarge() CreateItemResponseObject {
	return CreateItem413ApplicationProblemPlusJSONResponse(newProblem(http.StatusRequestEntityTooLarge, problemTypeImageTooLarge,
		"Image too large", "L'image dépasse 20 Mo.", itemsPath))
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
	case errors.Is(err, capture.ErrUnsupportedImage):
		return badRequest(problemTypeUnsupportedImage, "Unsupported image", "L'image doit être au format JPEG, PNG, HEIC ou WebP."), nil
	case errors.Is(err, capture.ErrImageTooLarge):
		return imageTooLarge(), nil
	case errors.As(err, &validation):
		return badRequest(problemTypeBlank, "Bad request", "La requête est invalide."), nil
	default:
		return nil, fmt.Errorf("create item: %w", err)
	}
}

// itemFromStore maps a stored item to the Item of the contract. Every nullable member is set
// explicitly: an unset nullable.Nullable would encode as its zero value, not as null. Arrays
// are empty, never null. Content type, category, tags and fiche have no column yet.
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
		Assets:           assetsFromStore(item.Assets),
		CreatedAt:        item.CreatedAt,
		UpdatedAt:        item.UpdatedAt,
		ExtractedAt:      nullableOf(item.ExtractedAt),
		AnalyzedAt:       nullableOf(item.AnalyzedAt),
		ReadAt:           nullableOf(item.ReadAt),
	}
}

// assetsFromStore maps the assets of an item; an item without asset has an empty array.
func assetsFromStore(assets []store.Asset) []Asset {
	mapped := make([]Asset, 0, len(assets))
	for _, asset := range assets {
		mapped = append(mapped, Asset{
			Id:        asset.ID,
			MimeType:  AssetMimeType(asset.MimeType),
			Width:     nullableOf(intOf(asset.Width)),
			Height:    nullableOf(intOf(asset.Height)),
			SizeBytes: asset.SizeBytes,
		})
	}
	return mapped
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
