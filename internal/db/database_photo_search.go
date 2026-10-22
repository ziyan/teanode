package db

import (
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/lib/pq"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// Finding photos by words: the description a model wrote, the words it read
// in the picture, and what the server knows without a model (the place, the
// file name, the camera, the albums). A search reads two lists, the photos
// whose search document matches the words and the described photos whose
// description means something like them, and ranks the two together.

// photoSearchPlaceTerm is the part of a search document that is the place.
const photoSearchPlaceTerm = `setweight(to_tsvector('simple', photo.place_name), 'A') ||`

// photoSearchHeldBackDocumentExpression is the search document as a reader
// sees it who may not know where a photo was taken: only what cannot name a
// place. The place is out, and so is everything else that can say it: the
// description and the words read in the picture, which describe what is
// there, the file name, which a camera or a person may have called after the
// place, and the titles of the albums the photo is in, which include the
// owner's own and not only the one shared. What stays is the camera, the names
// of the people in the photo (the owner gave those names to the reader with the
// photo), and the title of the one album the reader is listing, which they
// reached by it. Its one argument is that album's identifier. A search must not
// find a photo by a word that would give its place away, any more than a
// listing may show it.
const photoSearchHeldBackDocumentExpression = `
	setweight(to_tsvector('simple', COALESCE((
		SELECT string_agg(photo_album.album_title, ' ')
		FROM photo_album_item JOIN photo_album ON photo_album.id = photo_album_item.photo_album_id
		WHERE photo_album_item.photo_id = photo.id AND photo_album.id = ?), '')), 'A') ||
	setweight(to_tsvector('simple', photo.camera_make || ' ' || photo.camera_model), 'C') ||` + photoFaceNamesSearchExpression + `

`

// photoSearchDocumentExpression is the search document of the photo the
// statement it is put in is about. The place and the albums' titles weigh
// most, the description and the words read in the picture next, the camera
// and the file name least. The "simple" configuration drops no word and
// stems none: one library mixes languages, and a person searches in their own
// words. The migration that added the column backfills with the same
// expression, less the names of the people in the photo (they weigh with the
// place), which no photo had then.
const photoSearchDocumentExpression = `
	` + photoSearchPlaceTerm + `
	setweight(to_tsvector('simple', COALESCE((
		SELECT string_agg(photo_album.album_title, ' ')
		FROM photo_album_item JOIN photo_album ON photo_album.id = photo_album_item.photo_album_id
		WHERE photo_album_item.photo_id = photo.id), '')), 'A') ||
	setweight(to_tsvector('simple', photo.photo_description || ' ' || photo.description_visible_text), 'B') ||
	setweight(to_tsvector('simple', photo.camera_make || ' ' || photo.camera_model || ' ' || photo.original_filename), 'C') ||` + photoFaceNamesSearchExpression

// refreshPhotoSearchDocuments makes the search document again of the photos a
// condition keeps. It is the one place a search document is written, and every
// write that changes what it is made of calls it: finding the place or the
// camera, describing, and putting a photo in an album, taking it out or naming
// the album again. A trigger could not see the albums.
func (self *transaction) refreshPhotoSearchDocuments(condition string, arguments ...any) error {
	return self.tx.Exec("UPDATE photo SET search_document = "+photoSearchDocumentExpression+" WHERE "+condition, arguments...).Error
}

// refreshPhotoSearchDocumentsOf is refreshPhotoSearchDocuments for named photos.
func (self *transaction) refreshPhotoSearchDocumentsOf(photoIds []string) error {
	if len(photoIds) == 0 {
		return nil
	}
	return self.refreshPhotoSearchDocuments("photo.id = ANY(?)", pq.Array(photoIds))
}

// photoSearchWords are the words of a search, checked.
func photoSearchWords(searchText string) ([]string, error) {
	words := strings.Fields(searchText)
	if len(words) > 8 {
		return nil, ErrInvalidArguments
	}
	for _, word := range words {
		if utf8.RuneCountInString(word) > 100 {
			return nil, ErrInvalidArguments
		}
	}
	return words, nil
}

// photoSearchPartCondition keeps the photos where every word is part of the
// place, the file name or the camera (the place and the file name of a photo
// only where the reader may learn its place): a document matches whole words only, and
// "harb" or "IMG_12" are still ways to look for a place or a file. It reads
// every photo the filters keep, so it is asked only when the document search
// leaves room.
func photoSearchPartCondition(words []string, scope photoScope) (string, []any) {
	arguments := []any{}
	parts := make([]string, 0, len(words))
	for _, word := range words {
		pattern := "%" + escapeLike(word) + "%"
		placeAndNameCondition := "photo.place_name ILIKE ? OR photo.original_filename ILIKE ?"
		if scope.isPlaceHeldBack {
			condition, conditionArguments := scope.placeShownCondition()
			placeAndNameCondition = "(" + condition + " AND (photo.place_name ILIKE ? OR photo.original_filename ILIKE ?))"
			arguments = append(arguments, conditionArguments...)
		}
		parts = append(parts, "("+placeAndNameCondition+" OR photo.camera_make ILIKE ? OR photo.camera_model ILIKE ?)")
		arguments = append(arguments, pattern, pattern, pattern, pattern)
	}
	return "(" + strings.Join(parts, " AND ") + ")", arguments
}

// searchDocument is the column a search reads, with the arguments it takes:
// the stored document, or for a listing that holds places back the document
// without the place for the photos whose place the reader may not learn.
func (self photoScope) searchDocument() (string, []any) {
	if !self.isPlaceHeldBack {
		return "photo.search_document", nil
	}
	condition, arguments := self.placeShownCondition()
	return "(CASE WHEN " + condition + " THEN photo.search_document ELSE (" + photoSearchHeldBackDocumentExpression + ") END)", append(arguments, self.photoAlbumId)
}

// rankPhotos is the identifiers of the photos a search finds, best first, at
// most MaximumRankedPhotos. The photos found by words come in the order of how
// well their document matches, newest first among equals, and those found by
// meaning in the order of how near their description is; a photo found by both
// is ranked by both lists' places, which is how the knowledge search ranks.
//
// The photos found by parts of words (see photoSearchPartCondition) come after
// those whose document matches, and are looked for only when the document
// search did not find as many photos as the caller needs: neededCount is how
// many of the best the caller reads. With a search by meaning the best are a
// mix of the two lists, so then the parts are skipped only when the document
// search filled the whole ranking.
func (self *transaction) rankPhotos(userId string, filter *PhotoFilter, neededCount int) ([]string, error) {
	words, err := photoSearchWords(filter.SearchText)
	if err != nil {
		return nil, err
	}
	searchText := strings.Join(words, " ")
	query, scope, err := self.photosMatchingInScope(userId, filter)
	if err != nil {
		return nil, err
	}
	documentSQL, documentArguments := scope.searchDocument()
	var keywordIds []string
	if err := query.Where(documentSQL+" @@ websearch_to_tsquery('simple', ?)", append(slices.Clone(documentArguments), searchText)...).
		Clauses(clause.OrderBy{Expression: clause.Expr{
			SQL:  "ts_rank_cd(" + documentSQL + ", websearch_to_tsquery('simple', ?)) DESC, " + photoTimelineExpression + " DESC, photo.id DESC",
			Vars: append(slices.Clone(documentArguments), searchText),
		}}).
		Limit(MaximumRankedPhotos).Pluck("photo.id", &keywordIds).Error; err != nil {
		return nil, err
	}
	hasMeaning := filter.SearchMeaning != nil && filter.SearchMeaning.ModelName != "" && len(filter.SearchMeaning.Vector) > 0
	if hasMeaning {
		neededCount = MaximumRankedPhotos
	}
	if len(keywordIds) < min(neededCount, MaximumRankedPhotos) && len(words) > 0 {
		partCondition, partArguments := photoSearchPartCondition(words, scope)
		partQuery, err := self.photosMatching(userId, filter)
		if err != nil {
			return nil, err
		}
		partQuery = partQuery.Where(partCondition, partArguments...)
		if len(keywordIds) > 0 {
			partQuery = partQuery.Where("NOT (photo.id = ANY(?))", pq.Array(keywordIds))
		}
		var partIds []string
		if err := partQuery.Order(photoTimelineExpression+" DESC, photo.id DESC").Limit(MaximumRankedPhotos-len(keywordIds)).Pluck("photo.id", &partIds).Error; err != nil {
			return nil, err
		}
		keywordIds = append(keywordIds, partIds...)
	}
	var nearest []Scored
	if hasMeaning {
		meaning := filter.SearchMeaning
		// The same filters as the words, as a subquery: a photo outside the
		// dates asked for is not a near one.
		kept, err := self.photosMatching(userId, filter)
		if err != nil {
			return nil, err
		}
		nearest, err = self.Nearest(PhotoVectorTable, userId, meaning.ModelName, meaning.Vector, MaximumRankedPhotos, VectorQuery{
			Where:     []string{`"photo_id" IN (?)`},
			Arguments: []any{kept.Select("photo.id")},
			Floor:     photoMeaningFloor,
		})
		if err != nil {
			return nil, err
		}
	}
	scores := map[string]float64{}
	order := make([]string, 0, len(keywordIds)+len(nearest))
	add := func(photoId string, position int) {
		if _, isRanked := scores[photoId]; !isRanked {
			order = append(order, photoId)
		}
		scores[photoId] += 1 / float64(photoSearchFusionConstant+position+1)
	}
	for position, photoId := range keywordIds {
		add(photoId, position)
	}
	for position, found := range nearest {
		add(found.ID, position)
	}
	sort.SliceStable(order, func(left, right int) bool { return scores[order[left]] > scores[order[right]] })
	if len(order) > MaximumRankedPhotos {
		order = order[:MaximumRankedPhotos]
	}
	return order, nil
}

// listRankedPhotos is a page of a search, in the order of its ranking. A page
// is reached by Offset or after the last photo of the previous page.
func (self *transaction) listRankedPhotos(userId string, filter *PhotoFilter) ([]*models.Photo, error) {
	// A page read after a photo is placed by its position in the whole ranking.
	neededCount := MaximumRankedPhotos
	if filter.BeforePhotoID == "" {
		neededCount = filter.Offset + filter.PageSize
	}
	rankedIds, err := self.rankPhotos(userId, filter, neededCount)
	if err != nil {
		return nil, err
	}
	start := filter.Offset
	if filter.BeforePhotoID != "" {
		position := slices.Index(rankedIds, filter.BeforePhotoID)
		if position < 0 {
			return nil, ErrNotFound
		}
		start = position + 1
	}
	if start >= len(rankedIds) {
		return []*models.Photo{}, nil
	}
	pageIds := rankedIds[start:min(start+filter.PageSize, len(rankedIds))]
	var rows []photoModel
	if err := self.tx.Where("photo.id IN ?", pageIds).Find(&rows).Error; err != nil {
		return nil, err
	}
	byId := make(map[string]*photoModel, len(rows))
	for index := range rows {
		byId[rows[index].ID] = &rows[index]
	}
	photos := make([]*models.Photo, 0, len(pageIds))
	for _, photoId := range pageIds {
		if row := byId[photoId]; row != nil {
			photos = append(photos, photoFromModel(row))
		}
	}
	return photos, nil
}

func (self *transaction) CountUndescribedPhotos(userId string, filter *PhotoFilter) (int, error) {
	if filter == nil {
		return 0, ErrInvalidArguments
	}
	rangeOnly := *filter
	rangeOnly.SearchText, rangeOnly.SearchMeaning = "", nil
	query, err := self.photosMatching(userId, &rangeOnly)
	if err != nil {
		return 0, err
	}
	var count int64
	if err := query.Where("photo.described_at IS NULL").Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count), nil
}

func (self *transaction) RequestPhotoDescription(userId, photoId string) error {
	if userId == "" || photoId == "" {
		return ErrNotFound
	}
	// updated_at is left alone here and when the description is written: it
	// is part of the address of every preview, which a description does not
	// change.
	requested := self.tx.Model(&photoModel{}).Where("photo.id = ? AND photo.user_id = ? AND "+photoIsReadable, photoId, userId, photoMarkedForDeletion).
		Update("description_requested_at", photoNow())
	if requested.Error != nil {
		return requested.Error
	}
	if requested.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (self *transaction) ClearPhotoDescriptionRequest(userId, photoId string) error {
	if userId == "" || photoId == "" {
		return ErrNotFound
	}
	return self.tx.Model(&photoModel{}).Where("photo.id = ? AND photo.user_id = ?", photoId, userId).Update("description_requested_at", nil).Error
}

func (self *transaction) SetPhotoDescription(userId, photoId string, description *PhotoDescription) error {
	if description == nil || utf8.RuneCountInString(description.PhotoDescription) > MaximumPhotoDescriptionCharacters ||
		utf8.RuneCountInString(description.VisibleText) > MaximumPhotoVisibleTextCharacters || len(description.ModelName) > 128 {
		return ErrInvalidArguments
	}
	if userId == "" || photoId == "" {
		return ErrNotFound
	}
	updated := self.tx.Model(&photoModel{}).Where("photo.id = ? AND photo.user_id = ? AND "+photoIsReadable, photoId, userId, photoMarkedForDeletion).Updates(map[string]any{
		"photo_description":        description.PhotoDescription,
		"description_visible_text": description.VisibleText,
		"description_model":        description.ModelName,
		"described_at":             photoNow(),
		"description_requested_at": nil,
	})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected == 0 {
		return ErrNotFound
	}
	if err := self.tx.Exec(`DELETE FROM "photo_vector" WHERE "photo_id" = ?`, photoId).Error; err != nil {
		return err
	}
	return self.refreshPhotoSearchDocumentsOf([]string{photoId})
}

func (self *transaction) PutPhotoVector(userId, photoId, model string, vector []float32) error {
	if userId == "" || photoId == "" || model == "" || len(vector) == 0 || len(model) > 200 {
		return ErrInvalidArguments
	}
	var ownedCount int64
	if err := self.tx.Model(&photoModel{}).Where("photo.id = ? AND photo.user_id = ? AND "+photoIsReadable, photoId, userId, photoMarkedForDeletion).Count(&ownedCount).Error; err != nil {
		return err
	}
	if ownedCount == 0 {
		return ErrNotFound
	}
	if err := self.NoteVectorModel(model, len(vector)); err != nil {
		return err
	}
	// One model's reading of the description at a time: a vector of another
	// model ranks nothing while this one is configured.
	if err := self.tx.Exec(`DELETE FROM "photo_vector" WHERE "photo_id" = ? AND "model" <> ?`, photoId, model).Error; err != nil {
		return err
	}
	return self.tx.Exec(`
		INSERT INTO "photo_vector" ("photo_id", "user_id", "model", "vector", "created_at")
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT ("photo_id", "model") DO UPDATE SET "vector" = EXCLUDED."vector", "created_at" = EXCLUDED."created_at"`,
		photoId, userId, model, pq.Float32Array(vector), photoNow()).Error
}
