package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/picture"
)

// The photo description job: a model looks at a photo's large preview and
// writes what is in it, with the words it can read there, and says whether it
// is a document the person keeps records of. The description is kept on the
// photo, embedded like other text, and searched from then on; a document is
// handed to the job that records it (photo_reading.go). A photo is described
// when it is new (photo_reading.go) or when somebody works with it (the agent
// looks at it, or the person asks)
// (docs/decisions/20261031-a-new-photo-is-read-as-it-arrives.md).

// QueuePhotoDescription asks for a photo of the agent's person to be
// described. The photo is marked as asked for, which is how a viewer knows to
// show that a description is on its way, and a photo already waiting for its
// job is not queued twice.
func (self *Agent) QueuePhotoDescription(tx db.Transaction, agentId, photoId string) (*models.AgentJob, error) {
	found, err := tx.GetAgent(agentId)
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, db.ErrNotFound
	}
	if err := tx.RequestPhotoDescription(found.UserID, photoId); err != nil {
		return nil, err
	}
	return self.Enqueue(tx, models.AgentJobDescribePhoto, agentId, "", photoId)
}

// PhotoDescriptionUnavailableMessage says why photos cannot be described, in
// words for the person, or "" when they can: the agent is on, and a model for
// the bulk work that reads pictures is configured.
func (self *Agent) PhotoDescriptionUnavailableMessage(configuration *config.Configuration) string {
	if self == nil || !configuration.Agent.Enabled {
		return "Describing photos needs the assistant to be on."
	}
	if self.receiptModelReady(configuration) != "" {
		return "Describing photos needs a model that reads pictures, and none is set up."
	}
	return ""
}

// photoDescriptionAnswer is what the model is asked for.
type photoDescriptionAnswer struct {
	PhotoDescription string         `json:"photoDescription"`
	VisibleText      describedWords `json:"visibleText"`
	DocumentKind     string         `json:"documentKind"`
}

// photoDocumentKind is the kind the answer gave, none for one it did not
// or one the server does not know.
func (self *photoDescriptionAnswer) photoDocumentKind() models.PhotoDocumentKind {
	documentKind := models.PhotoDocumentKind(strings.TrimSpace(self.DocumentKind))
	if !documentKind.IsValid() {
		return models.PhotoDocumentNone
	}
	return documentKind
}

// describedWords is the words read in a picture: a string, one piece of text
// to a line, or a list of pieces, which some models answer with instead.
type describedWords string

// UnmarshalJSON takes a string, a list of strings or null.
func (self *describedWords) UnmarshalJSON(encoded []byte) error {
	var text string
	if err := json.Unmarshal(encoded, &text); err == nil {
		*self = describedWords(text)
		return nil
	}
	var pieces []string
	if err := json.Unmarshal(encoded, &pieces); err != nil {
		if strings.TrimSpace(string(encoded)) == "null" {
			*self = ""
			return nil
		}
		return fmt.Errorf("visibleText is text, not %s", cutRunes(strings.TrimSpace(string(encoded)), 40))
	}
	*self = describedWords(strings.Join(pieces, "\n"))
	return nil
}

// runDescribePhoto is the handler for a photo description job.
func (self *Agent) runDescribePhoto(ctx context.Context, run *Run) error {
	photoId, ownerId := run.Job.SubjectID, run.Owner.ID
	if why := self.PhotoDescriptionUnavailableMessage(run.Configuration()); why != "" {
		return self.refusePhotoDescription(ctx, run, ownerId, photoId, why)
	}
	var photo *models.Photo
	var preview, videoPreview *models.PhotoPreview
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		var err error
		if photo, err = tx.GetPhoto(ownerId, photoId); err != nil {
			return err
		}
		preview, err = tx.GetPhotoPreview(ownerId, photoId, models.PhotoPreviewLarge)
		if errors.Is(err, db.ErrNotFound) {
			preview, err = nil, nil
		}
		if err != nil || photo.PhotoKind != models.PhotoKindVideo {
			return err
		}
		videoPreview, err = tx.GetPhotoPreview(ownerId, photoId, models.PhotoPreviewVideo)
		if errors.Is(err, db.ErrNotFound) {
			videoPreview, err = nil, nil
		}
		return err
	}); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil // the photo is gone
		}
		return err
	}
	// Written already and not asked for again: the job was queued twice, a
	// request came in while the first was running, or handing the document
	// on failed and the job is tried again for it, without asking the model
	// a second time.
	if photo.DescribedAt != nil && photo.DescriptionRequestedAt == nil {
		return self.handOnDocumentLeftOver(ctx, run, photo, preview)
	}
	if preview == nil {
		if photo.PhotoProcessingState == models.PhotoProcessingFailed {
			return self.refusePhotoDescription(ctx, run, ownerId, photoId, "its previews could not be made, so there is no picture to look at")
		}
		// Previews are made soon after an upload; the job tries again on the
		// ladder.
		return fmt.Errorf("the preview of photo %s is not made yet", photoId)
	}
	if run.Storage() == nil {
		return fmt.Errorf("there is no storage to read the picture of the photo from")
	}
	content, err := run.Storage().GetFile(ctx, models.PhotoPreviewFileID(photoId, models.PhotoPreviewLarge))
	if err != nil {
		return fmt.Errorf("the picture of photo %s cannot be read yet: %w", photoId, err)
	}
	if len(content) == 0 || len(content) > attachmentImageBytes {
		return self.refusePhotoDescription(ctx, run, ownerId, photoId, fmt.Sprintf("its picture is %s, which a model cannot be shown", formatBytes(int64(len(content)))))
	}
	pictures := []llm.ContentPart{{Type: "image", MediaType: preview.ContentType, Data: content}}
	if photo.PhotoKind == models.PhotoKindVideo {
		frames, err := videoFrames(ctx, run, photo, videoPreview)
		if err != nil {
			return err
		}
		if len(frames) > 0 {
			pictures = pictures[:0]
			for _, frame := range frames {
				pictures = append(pictures, llm.ContentPart{Type: "image", MediaType: "image/jpeg", Data: frame})
			}
		}
	}
	prompt, err := render("describe_photo.txt", map[string]any{
		"PersonName":      personName(run.Owner),
		"Language":        languageName(KnowledgeLanguage(run.Agent, run.Owner)),
		"PlaceName":       photo.PlaceName,
		"IsVideo":         photo.PhotoKind == models.PhotoKindVideo,
		"FrameCount":      len(pictures),
		"DurationSeconds": (photo.DurationMS + 500) / 1000,
	})
	if err != nil {
		return err
	}
	thinking, err := self.thinkAbout(ctx, run, "Describing a photo", prompt,
		pictures, noTools, 1, models.AgentJobDescribePhoto, config.AgentWorkScan)
	if err != nil {
		return err
	}
	answer := readModelAnswer[photoDescriptionAnswer](thinking.Text, "photoDescription")
	// A paystub's or a tax form's taxpayer number is kept nowhere: not in the
	// description, the words a search reads, nor the run, which keeps what
	// the model answered. The model may call such a form anything, so this
	// does not wait for it to say it is a tax document.
	description := cutRunes(strings.TrimSpace(finance.MaskTaxpayerNumbersInFile(answer.Value.PhotoDescription)), db.MaximumPhotoDescriptionCharacters)
	if thinking.Conversation != nil && finance.MaskTaxpayerNumbersInFile(thinking.Text) != thinking.Text {
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			_, err := tx.RedactAgentConversationMessages(thinking.Conversation.ID, finance.MaskTaxpayerNumbersInFile)
			return err
		}); err != nil {
			return err
		}
	}
	if !answer.IsValid || description == "" {
		problem := answer.Problem
		if answer.IsValid {
			problem = "the description is empty"
		}
		// Asking again would ask the same, so the request ends here and the
		// photo stays undescribed.
		self.retitle(ctx, run, thinking.Conversation, "Looked at a photo, and could not tell what the answer said: "+problem)
		return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			return tx.ClearPhotoDescriptionRequest(ownerId, photoId)
		})
	}
	visibleText := cutRunes(strings.TrimSpace(finance.MaskTaxpayerNumbersInFile(string(answer.Value.VisibleText))), db.MaximumPhotoVisibleTextCharacters)
	documentKind := answer.Value.photoDocumentKind()
	if photo.PhotoKind == models.PhotoKindVideo {
		// A frame of a video is no record of a purchase or an account, and
		// nothing downstream reads a video.
		documentKind = models.PhotoDocumentNone
	}
	// Embedded before the transaction: embedding is a call to another service.
	// Without an embedding model, or when it fails, the photo is still found
	// by its words.
	meaningText := strings.TrimSpace(strings.Join([]string{description, visibleText, photo.PlaceName}, "\n"))
	found := self.meaningOf(ctx, run.Agent.ID, string(models.AgentJobDescribePhoto), cutRunes(meaningText, graphEmbedCharacters))
	isPlaceChanged := false
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		// The person may have moved the photo while the model looked, and a
		// description of the old place must not land after the move cleared
		// it. The row is held until the description is written, so a move
		// waits for it or comes first. The request ends; asking again
		// describes the new place.
		current, err := tx.LockPhoto(ownerId, photoId)
		if err != nil {
			return err
		}
		if current.PlaceName != photo.PlaceName || current.IsLocationRemoved != photo.IsLocationRemoved {
			isPlaceChanged = true
			return tx.ClearPhotoDescriptionRequest(ownerId, photoId)
		}
		if err := tx.SetPhotoDescription(ownerId, photoId, &db.PhotoDescription{
			PhotoDescription: description, VisibleText: visibleText, ModelName: run.Configuration().Agent.Models.ForWork(config.AgentWorkScan),
			PhotoDocumentKind: documentKind,
		}); err != nil {
			return err
		}
		if found == nil {
			return nil
		}
		return tx.PutPhotoVector(ownerId, photoId, found.ModelName, found.Vector)
	}); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil // the photo was deleted while the model looked
		}
		return err
	}
	if isPlaceChanged {
		self.retitle(ctx, run, thinking.Conversation, "Looked at a photo whose location changed meanwhile, and kept nothing")
		return nil
	}
	title := "Described a photo: " + cutRunes(description, 120)
	if documentKind != models.PhotoDocumentNone {
		handedOn, err := self.handOnPhotoDocument(ctx, run, photo, documentKind, content, preview.ContentType)
		if err != nil {
			// The description is kept, and the job is tried again for the
			// document alone (handOnDocumentLeftOver).
			self.retitle(ctx, run, thinking.Conversation, title+" (a "+photoDocumentWords(documentKind)+", not handed on yet)")
			return fmt.Errorf("cannot hand on the %s in photo %s: %w", documentKind, photoId, err)
		}
		if handedOn != "" {
			title += " (a " + photoDocumentWords(documentKind) + ", " + handedOn + ")"
		}
	}
	self.retitle(ctx, run, thinking.Conversation, title)
	return nil
}

// How a video is shown to the model, which cannot watch one: frames spread
// across its preview, one for each second of a shorter clip, each at most
// videoFrameLongSidePixels on its long side. A preview larger than
// maximumVideoFramesSourceBytes is not read to cut them from, and the model
// sees the poster alone, as it does for a video too long to have a preview.
const (
	videoFrameCount               = 6
	videoFrameLongSidePixels      = 1024
	maximumVideoFramesSourceBytes = 128 << 20
)

// videoFrames is the frames of a video photo, in order, cut from its
// preview. None, and no error, when there is no preview or the frames cannot
// be cut: the video is then described by its poster.
func videoFrames(ctx context.Context, run *Run, photo *models.Photo, videoPreview *models.PhotoPreview) ([][]byte, error) {
	if videoPreview == nil || videoPreview.PreviewSizeBytes > maximumVideoFramesSourceBytes || photo.DurationMS <= 0 {
		return nil, nil
	}
	content, err := run.Storage().GetFile(ctx, models.PhotoPreviewFileID(photo.ID, models.PhotoPreviewVideo))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		log.Warningf("the video preview of photo %s could not be read, it is described by its poster: %s", photo.ID, err)
		return nil, nil
	}
	directory, err := os.MkdirTemp("", "teanode-video-frames-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(directory) }()
	path := filepath.Join(directory, "preview.mp4")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return nil, err
	}
	frameCount := int(min(int64(videoFrameCount), max(photo.DurationMS/1000, 1)))
	frames, err := picture.VideoFrames(ctx, path, photo.DurationMS, frameCount, videoFrameLongSidePixels)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		log.Warningf("the frames of video %s could not be cut, it is described by its poster: %s", photo.ID, err)
		return nil, nil
	}
	return frames, nil
}

// handOnDocumentLeftOver hands on the document of a photo described already
// whose handing on failed: one its description called a document, with no
// copy made of it yet.
func (self *Agent) handOnDocumentLeftOver(ctx context.Context, run *Run, photo *models.Photo, preview *models.PhotoPreview) error {
	if photo.PhotoDocumentKind == "" || photo.PhotoDocumentKind == models.PhotoDocumentNone || preview == nil || run.Storage() == nil {
		return nil
	}
	var existing *models.AgentAttachment
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		existing, err = tx.GetAgentAttachmentOfPhoto(run.Agent.ID, photo.ID)
		return err
	}); err != nil || existing != nil {
		return err
	}
	content, err := run.Storage().GetFile(ctx, models.PhotoPreviewFileID(photo.ID, models.PhotoPreviewLarge))
	if err != nil {
		return fmt.Errorf("the picture of photo %s cannot be read yet: %w", photo.ID, err)
	}
	_, err = self.handOnPhotoDocument(ctx, run, photo, photo.PhotoDocumentKind, content, preview.ContentType)
	return err
}

// photoDocumentWords is a document kind in a run's title.
func photoDocumentWords(documentKind models.PhotoDocumentKind) string {
	switch documentKind {
	case models.PhotoDocumentReceipt:
		return "receipt"
	case models.PhotoDocumentStatement:
		return "statement"
	case models.PhotoDocumentTaxDocument:
		return "tax document"
	}
	return string(documentKind)
}

// refusePhotoDescription ends a request that cannot be served, saying why on
// the run and taking the photo's "describing" mark off.
func (self *Agent) refusePhotoDescription(ctx context.Context, run *Run, ownerId, photoId, why string) error {
	return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		if _, err := noteRun(tx, run, "Did not describe a photo: "+why); err != nil {
			return err
		}
		err := tx.ClearPhotoDescriptionRequest(ownerId, photoId)
		if errors.Is(err, db.ErrNotFound) {
			return nil
		}
		return err
	})
}

// EmbedPhotoSearch is the words of a photo search as the vector a described
// photo is compared to, with the name of the model that read them. Nil when
// the deployment has no embedding model or it did not answer: the search is
// then by words alone.
func (self *Agent) EmbedPhotoSearch(ctx context.Context, agent *models.Agent, searchText string) *db.PhotoSearchMeaning {
	if self.settings.Registry == nil {
		return nil
	}
	found := self.meaningOf(ctx, agent.ID, "photo_search", cutRunes(strings.TrimSpace(searchText), graphEmbedCharacters))
	if found == nil {
		return nil
	}
	return &db.PhotoSearchMeaning{ModelName: found.ModelName, Vector: found.Vector}
}
