package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// The photo description job: a model looks at a photo's large preview and
// writes what is in it, with the words it can read there. The description is
// kept on the photo, embedded like other text, and searched from then on. A
// photo is described only when somebody works with it (the agent looks at it,
// or the person asks), never in the background
// (docs/decisions/20261016-a-photo-is-described-when-somebody-looks-at-it.md).

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
	var preview *models.PhotoPreview
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		var err error
		if photo, err = tx.GetPhoto(ownerId, photoId); err != nil {
			return err
		}
		preview, err = tx.GetPhotoPreview(ownerId, photoId, models.PhotoPreviewLarge)
		if errors.Is(err, db.ErrNotFound) {
			preview, err = nil, nil
		}
		return err
	}); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil // the photo is gone
		}
		return err
	}
	// Written already and not asked for again: the job was queued twice, or a
	// request came in while the first was running.
	if photo.DescribedAt != nil && photo.DescriptionRequestedAt == nil {
		return nil
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
	prompt, err := render("describe_photo.txt", map[string]any{
		"PersonName": personName(run.Owner),
		"Language":   languageName(KnowledgeLanguage(run.Agent, run.Owner)),
		"PlaceName":  photo.PlaceName,
		"IsVideo":    photo.PhotoKind == models.PhotoKindVideo,
	})
	if err != nil {
		return err
	}
	thinking, err := self.thinkAbout(ctx, run, "Describing a photo", prompt,
		[]llm.ContentPart{{Type: "image", MediaType: preview.ContentType, Data: content}}, noTools, 1,
		models.AgentJobDescribePhoto, config.AgentWorkScan)
	if err != nil {
		return err
	}
	answer := readModelAnswer[photoDescriptionAnswer](thinking.Text, "photoDescription")
	description := cutRunes(strings.TrimSpace(answer.Value.PhotoDescription), db.MaximumPhotoDescriptionCharacters)
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
	visibleText := cutRunes(strings.TrimSpace(string(answer.Value.VisibleText)), db.MaximumPhotoVisibleTextCharacters)
	// Embedded before the transaction: embedding is a call to another service.
	// Without an embedding model, or when it fails, the photo is still found
	// by its words.
	meaningText := strings.TrimSpace(strings.Join([]string{description, visibleText, photo.PlaceName}, "\n"))
	found := self.meaningOf(ctx, run.Agent.ID, string(models.AgentJobDescribePhoto), cutRunes(meaningText, graphEmbedCharacters))
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		if err := tx.SetPhotoDescription(ownerId, photoId, &db.PhotoDescription{
			PhotoDescription: description, VisibleText: visibleText, ModelName: run.Configuration().Agent.Models.ForWork(config.AgentWorkScan),
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
	self.retitle(ctx, run, thinking.Conversation, "Described a photo: "+cutRunes(description, 120))
	return nil
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
