package photo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// The server side of the resumable upload draft
// (draft-ietf-httpbis-resumable-upload), which is what a phone's background
// upload speaks and what the dashboard and the command line speak too.
//
// A POST to the collection starts an upload and sends the first bytes; a
// 104 tells the client where the upload is, so that it can resume if the
// answer never comes. A HEAD says how many bytes the server has, and a PATCH
// sends the rest from there. Bytes are kept in parts as they arrive, and
// only a whole part counts towards the offset a client is told, so a
// dropped connection costs at most the part in flight.

// statusUploadResumptionSupported is the 104 the draft defines, which Go has
// no name for.
const statusUploadResumptionSupported = 104

// readDeadlineExtension is how much longer a request may take to send each
// part. The server gives a request five minutes to arrive in full, and
// a file of gigabytes on a slow link takes longer than that.
const readDeadlineExtension = 10 * time.Minute

// completionTimeout bounds finishing an upload: reading the parts back and
// storing the file. It does not end when the client goes away, because the
// client usually has by then, and the photo should exist anyway.
const completionTimeout = 30 * time.Minute

// errBodyCut is a request body that ended before it should have, because
// the connection dropped.
var errBodyCut = errors.New("photo: the request body was cut short")

// ErrTooManyParts is an upload that came in more pieces than a file of the
// largest size needs twice over.
var ErrTooManyParts = errors.New("photo: the upload is in too many parts; send it in parts of 8 MiB")

// errOffsetMoved is an append that found the upload somewhere other than where
// the request believed.
var errOffsetMoved = errors.New("photo: the upload is not where the request thinks it is")

var pieceBuffers = sync.Pool{New: func() any { buffer := make([]byte, models.PhotoUploadPartBytes); return &buffer }}

// ServeCollection answers the requests for the collection of uploads, for a
// person already signed in.
func (self *Store) ServeCollection(response http.ResponseWriter, request *http.Request, userId string) {
	interopVersion, problem := parseInteropVersion(request)
	if problem != nil {
		http.Error(response, problem.Error(), http.StatusBadRequest)
		return
	}
	if interopVersion > 0 {
		response.Header().Set(headerInteropVersion, strconv.Itoa(interopVersion))
	}
	switch request.Method {
	case http.MethodOptions:
		response.Header().Set("Allow", "OPTIONS, POST")
		self.setLimit(response, interopVersion)
		response.WriteHeader(http.StatusOK)
	case http.MethodPost:
		self.serveCreate(response, request, userId, interopVersion)
	default:
		response.Header().Set("Allow", "OPTIONS, POST")
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ServeUpload answers the requests for one upload.
func (self *Store) ServeUpload(response http.ResponseWriter, request *http.Request, userId, photoUploadId string) {
	interopVersion, problem := parseInteropVersion(request)
	if problem != nil {
		http.Error(response, problem.Error(), http.StatusBadRequest)
		return
	}
	if interopVersion > 0 {
		response.Header().Set(headerInteropVersion, strconv.Itoa(interopVersion))
	}
	switch request.Method {
	case http.MethodOptions:
		response.Header().Set("Allow", "OPTIONS, HEAD, PATCH, DELETE")
		self.setLimit(response, interopVersion)
		response.WriteHeader(http.StatusOK)
	case http.MethodHead:
		self.serveStatus(response, request, userId, photoUploadId)
	case http.MethodPatch:
		self.serveAppend(response, request, userId, photoUploadId, interopVersion)
	case http.MethodDelete:
		self.serveCancel(response, request, userId, photoUploadId)
	default:
		response.Header().Set("Allow", "OPTIONS, HEAD, PATCH, DELETE")
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// setLimit says how large a file may be. The draft wrote the limit as a bare
// number until revision 5, which wrote it as a named parameter, and a client
// reads the form of the revision it speaks.
func (self *Store) setLimit(response http.ResponseWriter, interopVersion int) {
	maximumFileBytes := int64(self.configuration().Photos.MaxFileBytes)
	if interopVersion >= 5 {
		response.Header().Set(headerUploadLimit, "max-size="+strconv.FormatInt(maximumFileBytes, 10))
	} else {
		response.Header().Set(headerUploadLimit, strconv.FormatInt(maximumFileBytes, 10))
	}
}

func (self *Store) serveCreate(response http.ResponseWriter, request *http.Request, userId string, interopVersion int) {
	maximumFileBytes := int64(self.configuration().Photos.MaxFileBytes)
	headers, problem := parseUploadHeaders(request)
	if problem != nil {
		http.Error(response, problem.Error(), http.StatusBadRequest)
		return
	}
	// A file of a kind that is not kept is refused before any of it is sent,
	// when the request names its kind. One that names none is looked at
	// when it has all arrived.
	if err := checkDeclaredContentType(headers.ContentType); err != nil {
		self.writeProblem(response, err)
		return
	}
	isComplete, problem := parseCompleteness(request)
	if problem != nil {
		http.Error(response, problem.Error(), http.StatusBadRequest)
		return
	}
	declaredBytes, problem := parseByteCount(request, headerUploadLength)
	if problem != nil {
		http.Error(response, problem.Error(), http.StatusBadRequest)
		return
	}
	// The length of the file is known from the request when it carries all of
	// it, which is every plain upload.
	if declaredBytes == nil && isComplete && request.ContentLength >= 0 {
		declaredBytes = &request.ContentLength
	}
	if (declaredBytes != nil && *declaredBytes > maximumFileBytes) || request.ContentLength > maximumFileBytes {
		self.writeProblem(response, &FileTooLargeError{LimitBytes: maximumFileBytes})
		return
	}
	if declaredBytes != nil {
		if err := self.checkRoomFor(request.Context(), userId, headers, *declaredBytes); err != nil {
			self.writeProblem(response, err)
			return
		}
	}
	var upload *models.PhotoUpload
	if err := self.database.TransactionContext(request.Context(), func(transaction db.Transaction) (err error) {
		upload, err = transaction.CreatePhotoUpload(&models.PhotoUpload{UserID: userId, DeclaredSizeBytes: declaredBytes, PhotoUploadHeaders: *headers})
		return err
	}); err != nil {
		self.writeProblem(response, err)
		return
	}
	location := self.absoluteUrl(request, api.PhotoUploadPath(upload.ID))
	response.Header().Set("Location", location)
	// The client learns where the upload is before it has been answered, and
	// if the answer never comes, resumes from there.
	response.WriteHeader(statusUploadResumptionSupported)
	self.receiveAndRespond(response, request, userId, upload, isComplete, interopVersion)
}

func (self *Store) serveStatus(response http.ResponseWriter, request *http.Request, userId, photoUploadId string) {
	upload, ok := self.readUpload(response, request, userId, photoUploadId)
	if !ok {
		return
	}
	self.setProgress(response, upload)
	response.WriteHeader(http.StatusNoContent)
}

func (self *Store) serveCancel(response http.ResponseWriter, request *http.Request, userId, photoUploadId string) {
	if err := self.database.TransactionContext(request.Context(), func(transaction db.Transaction) error {
		_, err := transaction.FailPhotoUpload(userId, photoUploadId)
		return err
	}); err != nil {
		self.writeProblem(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (self *Store) serveAppend(response http.ResponseWriter, request *http.Request, userId, photoUploadId string, interopVersion int) {
	upload, ok := self.readUpload(response, request, userId, photoUploadId)
	if !ok {
		return
	}
	// A client that sent the type the draft names, or none, or the plain type
	// a program sends bytes with, is understood; the rest do not know the
	// protocol and are told so.
	if contentType := request.Header.Get("Content-Type"); contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil || (mediaType != contentTypePartialBytes && mediaType != "application/octet-stream") {
			http.Error(response, "Content-Type: must be "+contentTypePartialBytes, http.StatusUnsupportedMediaType)
			return
		}
	}
	offsetBytes, problem := parseByteCount(request, headerUploadOffset)
	if problem == nil && offsetBytes == nil {
		problem = &headerError{headerUploadOffset, "is required: the number of bytes the server already has"}
	}
	if problem != nil {
		http.Error(response, problem.Error(), http.StatusBadRequest)
		return
	}
	isComplete, problem := parseCompleteness(request)
	if problem != nil {
		http.Error(response, problem.Error(), http.StatusBadRequest)
		return
	}
	if upload.PhotoUploadState != models.PhotoUploadReceiving || *offsetBytes != upload.ReceivedSizeBytes {
		self.writeOffsetConflict(response, upload)
		return
	}
	self.receiveAndRespond(response, request, userId, upload, isComplete, interopVersion)
}

// receiveAndRespond reads the request's body into the upload, completes it
// if the request carried the end, and answers.
func (self *Store) receiveAndRespond(response http.ResponseWriter, request *http.Request, userId string, upload *models.PhotoUpload, isComplete bool, interopVersion int) {
	if _, isWriting := self.writingUploads.LoadOrStore(upload.ID, struct{}{}); isWriting {
		self.writeCurrentOffsetConflict(request.Context(), response, userId, upload)
		return
	}
	defer self.writingUploads.Delete(upload.ID)

	upload, hasEnded, err := self.receiveBody(request.Context(), http.NewResponseController(response), userId, upload, request.Body, request.ContentLength)
	if err != nil {
		if errors.Is(err, errBodyCut) {
			// Nobody is listening for an answer. What was whole is kept, and
			// the client asks where it got to.
			log.Debugf("an upload of %s was cut after %d bytes: %s", upload.ID, upload.ReceivedSizeBytes, err)
			return
		}
		if errors.Is(err, errOffsetMoved) {
			self.writeCurrentOffsetConflict(request.Context(), response, userId, upload)
			return
		}
		self.failAndWriteProblem(request.Context(), response, userId, upload, err)
		return
	}
	if !hasEnded {
		return
	}
	if !isComplete {
		self.setProgress(response, upload)
		if request.Method == http.MethodPatch {
			response.WriteHeader(http.StatusNoContent)
			return
		}
		response.Header().Set("Location", self.absoluteUrl(request, api.PhotoUploadPath(upload.ID)))
		response.WriteHeader(http.StatusCreated)
		return
	}
	completionContext, cancel := context.WithTimeout(context.WithoutCancel(request.Context()), completionTimeout)
	defer cancel()
	photo, err := self.completeUpload(completionContext, userId, upload)
	if err != nil {
		self.failAndWriteProblem(completionContext, response, userId, upload, err)
		return
	}
	upload.PhotoUploadState = models.PhotoUploadComplete
	upload.PhotoID = &photo.ID
	self.setProgress(response, upload)
	response.Header().Set("Location", self.absoluteUrl(request, api.PhotoUploadPath(upload.ID)))
	response.Header().Set(headerPhotoId, photo.ID)
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(response).Encode(map[string]string{"photoId": photo.ID, "photoUploadId": upload.ID})
}

// receiveBody stores the body as parts. It returns the upload as it then is
// and whether the body ended where it should: an error says a part could not
// be stored, or the connection dropped, which is errBodyCut and not worth
// reporting. Bytes read of a part that did not come whole are dropped.
func (self *Store) receiveBody(ctx context.Context, controller *http.ResponseController, userId string, upload *models.PhotoUpload, body io.Reader, contentLength int64) (*models.PhotoUpload, bool, error) {
	maximumFileBytes := int64(self.configuration().Photos.MaxFileBytes)
	// The upload says how many parts it has, so they are not listed to find
	// the next index. A request that finds the upload moved is told so when it
	// stores a part.
	partIndex := upload.PartCount
	maximumPartCount := maximumPartCountOf(maximumFileBytes)
	bufferPointer := pieceBuffers.Get().(*[]byte)
	defer pieceBuffers.Put(bufferPointer)
	buffer := *bufferPointer
	if contentLength >= 0 && contentLength < int64(len(buffer)) {
		buffer = buffer[:contentLength+1]
	}
	for {
		_ = controller.SetReadDeadline(time.Now().Add(readDeadlineExtension))
		filledBytes, err := fillBuffer(body, buffer)
		if err != nil && !errors.Is(err, io.EOF) {
			return upload, false, fmt.Errorf("%w: %s", errBodyCut, err)
		}
		hasEnded := err != nil
		if filledBytes > 0 {
			if upload.ReceivedSizeBytes+int64(filledBytes) > maximumFileBytes {
				return upload, false, &FileTooLargeError{LimitBytes: maximumFileBytes}
			}
			if upload.DeclaredSizeBytes != nil && upload.ReceivedSizeBytes+int64(filledBytes) > *upload.DeclaredSizeBytes {
				return upload, false, ErrWrongSize
			}
			if partIndex >= maximumPartCount {
				return upload, false, ErrTooManyParts
			}
			if err := self.checkRoomForPart(ctx, userId, upload, int64(filledBytes)); err != nil {
				return upload, false, err
			}
			updated, err := self.storePart(ctx, userId, upload, partIndex, buffer[:filledBytes])
			if err != nil {
				return upload, false, err
			}
			upload = updated
			partIndex++
		}
		if hasEnded {
			return upload, true, nil
		}
	}
}

// fillBuffer reads until the buffer is full or the body ends. A body that
// ends cleanly returns io.EOF with what it read; one that was cut returns
// the transport's error, which is how a short last part is told from a
// part that never finished.
func fillBuffer(body io.Reader, buffer []byte) (int, error) {
	filledBytes := 0
	for filledBytes < len(buffer) {
		count, err := body.Read(buffer[filledBytes:])
		filledBytes += count
		if err != nil {
			return filledBytes, err
		}
	}
	return filledBytes, nil
}

// storePart keeps one whole part and its receipt. The context is not the
// request's: the part is whole, and the connection dropping after the last
// byte of it is no reason to lose it.
func (self *Store) storePart(ctx context.Context, userId string, upload *models.PhotoUpload, partIndex int, content []byte) (*models.PhotoUpload, error) {
	storeContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	digest := sha256.Sum256(content)
	part := &models.PhotoUploadPart{PhotoUploadID: upload.ID, PartIndex: partIndex, PartSizeBytes: int64(len(content)), PartSHA256: hex.EncodeToString(digest[:])}
	if err := self.files.PutFileStream(storeContext, models.PhotoUploadPartFileID(upload.ID, partIndex), bytes.NewReader(content), int64(len(content))); err != nil {
		return upload, err
	}
	var updated *models.PhotoUpload
	err := self.database.TransactionContext(storeContext, func(transaction db.Transaction) (err error) {
		updated, err = transaction.AppendPhotoUploadPart(userId, part, upload.ReceivedSizeBytes)
		return err
	})
	if errors.Is(err, db.ErrAlreadyExists) {
		return upload, errOffsetMoved
	}
	if err != nil {
		return upload, err
	}
	return updated, nil
}

// checkRoomFor refuses a file the account has no room for before any of it
// is sent. The room is what the limit leaves after the library and after the
// bytes of the account's other uploads that have not finished: those are not
// photos yet, and counting only photos would let any number of uploads each
// pass the check and then fill the store. A file that replaces one the photo
// already has is let through: whether it fits is settled when it arrives, and a
// client retrying a file that is already stored must not be turned away at the
// door.
func (self *Store) checkRoomFor(ctx context.Context, userId string, headers *models.PhotoUploadHeaders, sizeBytes int64) error {
	maximumAccountBytes := int64(self.configuration().Photos.MaxAccountBytes)
	if maximumAccountBytes <= 0 {
		return nil
	}
	return self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
		isReplacement, err := hasFileInRole(transaction, userId, headers)
		if err != nil || isReplacement {
			return err
		}
		isDuplicate, err := isDuplicateOfOwnOriginal(transaction, userId, headers, sizeBytes, true)
		if err != nil || isDuplicate {
			return err
		}
		usedBytes, err := transaction.PhotoUsedBytes(userId)
		if err != nil {
			return err
		}
		// Starting this upload ends an unfinished one of the same file, so
		// that one is not counted.
		openBytes, err := transaction.PhotoOpenUploadBytes(userId, headers.ClientPhotoID, headers.PhotoFileRole, self.now().Add(-openUploadQuotaWindow))
		if err != nil {
			return err
		}
		if usedBytes+openBytes > maximumAccountBytes-sizeBytes {
			if usedBytes > maximumAccountBytes-sizeBytes {
				return &QuotaError{LimitBytes: maximumAccountBytes}
			}
			return self.otherUploadsError(transaction, userId, headers, maximumAccountBytes, self.now().Add(-openUploadQuotaWindow), openUploadQuotaWindow)
		}
		return self.checkUnfinishedBytes(transaction, userId, headers, maximumAccountBytes)
	})
}

// checkUnfinishedBytes refuses an upload while the account's other unfinished
// uploads, of any age, hold more than its limit. The check above leaves out the
// uploads that have sat idle for a day, so that they do not hold room a file
// being sent needs, but they are still bytes in the store until cleanup deletes
// them a week on, and without this an account could keep several times its
// limit in parts that each look idle. The upload's own bytes are not added: an
// account whose idle uploads fit its limit is not turned away for a file the
// first check lets in.
func (self *Store) checkUnfinishedBytes(transaction db.Transaction, userId string, headers *models.PhotoUploadHeaders, maximumAccountBytes int64) error {
	unfinishedBytes, err := transaction.PhotoOpenUploadBytes(userId, headers.ClientPhotoID, headers.PhotoFileRole, time.Unix(0, 0))
	if err != nil {
		return err
	}
	if unfinishedBytes > maximumAccountBytes {
		return self.otherUploadsError(transaction, userId, headers, maximumAccountBytes, time.Unix(0, 0), idleUploadRetention)
	}
	return nil
}

// otherUploadsError is the refusal that the account's other unfinished uploads
// cause: not the file's fault, and over when the oldest of them is gone, which
// is when it has sat idle for retention since its last part, so that is when
// the client is told to ask again.
func (self *Store) otherUploadsError(transaction db.Transaction, userId string, headers *models.PhotoUploadHeaders, maximumAccountBytes int64, updatedAfter time.Time, retention time.Duration) error {
	wait := minimumQuotaRetryAfter
	oldestAt, hasOldest, err := transaction.PhotoOldestOpenUploadAt(userId, headers.ClientPhotoID, headers.PhotoFileRole, updatedAfter)
	if err != nil {
		return err
	}
	if hasOldest {
		wait = oldestAt.Add(retention).Sub(self.now())
	}
	return &QuotaError{LimitBytes: maximumAccountBytes, IsCausedByOtherUploads: true, RetryAfter: min(max(wait, minimumQuotaRetryAfter), maximumQuotaRetryAfter)}
}

// checkRoomForPart is checkRoomFor for a part that has arrived: the bytes the
// upload already holds, and the part, are added to the library and to the
// bytes of the account's other unfinished uploads. It is asked of every part,
// because a file whose size was not declared, or was declared falsely, is only
// known as it comes. A refusal that the upload's own bytes do not account for
// (it would fit alone) is the other uploads', and is marked so.
func (self *Store) checkRoomForPart(ctx context.Context, userId string, upload *models.PhotoUpload, partBytes int64) error {
	maximumAccountBytes := int64(self.configuration().Photos.MaxAccountBytes)
	if maximumAccountBytes <= 0 {
		return nil
	}
	return self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
		isReplacement, err := hasFileInRole(transaction, userId, &upload.PhotoUploadHeaders)
		if err != nil || isReplacement {
			return err
		}
		isDuplicate, err := isDuplicateOfOwnOriginal(transaction, userId, &upload.PhotoUploadHeaders, upload.ReceivedSizeBytes+partBytes, false)
		if err != nil || isDuplicate {
			return err
		}
		usedBytes, err := transaction.PhotoUsedBytes(userId)
		if err != nil {
			return err
		}
		otherBytes, err := transaction.PhotoOpenUploadBytes(userId, upload.PhotoUploadHeaders.ClientPhotoID, upload.PhotoUploadHeaders.PhotoFileRole, self.now().Add(-openUploadQuotaWindow))
		if err != nil {
			return err
		}
		ownBytes := upload.ReceivedSizeBytes + partBytes
		if usedBytes+otherBytes+ownBytes > maximumAccountBytes {
			if usedBytes+ownBytes > maximumAccountBytes {
				return &QuotaError{LimitBytes: maximumAccountBytes}
			}
			return self.otherUploadsError(transaction, userId, &upload.PhotoUploadHeaders, maximumAccountBytes, self.now().Add(-openUploadQuotaWindow), openUploadQuotaWindow)
		}
		return self.checkUnfinishedBytes(transaction, userId, &upload.PhotoUploadHeaders, maximumAccountBytes)
	})
}

// isDuplicateOfOwnOriginal says an upload is, as far as can be told before it
// has all arrived, the bytes the person already has as the original of a photo,
// which when they are complete are recorded against that photo and cost no room.
// The request names the digest it will have (headerDeclaredSha256); that is
// believed only as far as the bytes stored under it: the size of what is sent
// must be the size of those bytes (exactly, when the length is declared, and not
// over it as the parts arrive), so that a request cannot use a digest it does
// not have the bytes of to send gigabytes past the limit. A file that is not an
// original is a new file of its photo, and costs its size.
func isDuplicateOfOwnOriginal(transaction db.Transaction, userId string, headers *models.PhotoUploadHeaders, sizeBytes int64, isSizeExact bool) (bool, error) {
	if headers.DeclaredSHA256 == "" || headers.PhotoFileRole != models.PhotoFileRoleOriginal {
		return false, nil
	}
	if _, err := transaction.FindPhotoByOriginalBlob(userId, headers.DeclaredSHA256); errors.Is(err, db.ErrNotFound) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	blob, err := transaction.GetPhotoBlob(headers.DeclaredSHA256)
	if errors.Is(err, db.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if isSizeExact {
		return sizeBytes == blob.BlobSizeBytes, nil
	}
	return sizeBytes <= blob.BlobSizeBytes, nil
}

// hasFileInRole says whether the uploader's photo is there already with a file
// in the role the upload is for.
func hasFileInRole(transaction db.Transaction, userId string, headers *models.PhotoUploadHeaders) (bool, error) {
	photos, err := transaction.FindPhotosByClientIDs(userId, []string{headers.ClientPhotoID})
	if err != nil || len(photos) == 0 {
		return false, err
	}
	if _, err := transaction.GetPhotoFile(userId, photos[0].ID, headers.PhotoFileRole); err == nil {
		return true, nil
	} else if !errors.Is(err, db.ErrNotFound) {
		return false, err
	}
	return false, nil
}

// maximumPartCountOf is how many parts one upload may have: twice as many as
// the largest file needs when every part is full, and a few more. Parts are
// made as the bytes of a request arrive, so a client that sends a byte at a
// time would otherwise make a row and an object for each.
func maximumPartCountOf(maximumFileBytes int64) int {
	return int(2*maximumFileBytes/models.PhotoUploadPartBytes) + 16
}

// readUpload reads the upload, or answers that there is none: one that is
// somebody else's, that failed, or that was cancelled is not found.
func (self *Store) readUpload(response http.ResponseWriter, request *http.Request, userId, photoUploadId string) (*models.PhotoUpload, bool) {
	var upload *models.PhotoUpload
	if err := self.database.TransactionContext(request.Context(), func(transaction db.Transaction) (err error) {
		upload, err = transaction.GetPhotoUpload(userId, photoUploadId)
		return err
	}); err != nil {
		self.writeProblem(response, err)
		return nil, false
	}
	if upload.PhotoUploadState == models.PhotoUploadFailed {
		http.Error(response, "this upload was cancelled or failed; start it again", http.StatusNotFound)
		return nil, false
	}
	return upload, true
}

// setProgress says where the upload is.
func (self *Store) setProgress(response http.ResponseWriter, upload *models.PhotoUpload) {
	response.Header().Set(headerUploadOffset, strconv.FormatInt(upload.ReceivedSizeBytes, 10))
	if upload.PhotoUploadState == models.PhotoUploadComplete {
		response.Header().Set(headerUploadComplete, "?1")
		if upload.PhotoID != nil {
			response.Header().Set(headerPhotoId, *upload.PhotoID)
		}
	} else {
		response.Header().Set(headerUploadComplete, "?0")
	}
	if upload.DeclaredSizeBytes != nil {
		response.Header().Set(headerUploadLength, strconv.FormatInt(*upload.DeclaredSizeBytes, 10))
	}
}

// writeOffsetConflict answers a request that disagrees with the upload about
// how much of it there is, with the figure it should have used.
func (self *Store) writeOffsetConflict(response http.ResponseWriter, upload *models.PhotoUpload) {
	self.setProgress(response, upload)
	http.Error(response, "the upload is at "+strconv.FormatInt(upload.ReceivedSizeBytes, 10)+" bytes; continue from there", http.StatusConflict)
}

// writeCurrentOffsetConflict is writeOffsetConflict for a request that was
// beaten to the upload by another, whose copy of it is out of date.
func (self *Store) writeCurrentOffsetConflict(ctx context.Context, response http.ResponseWriter, userId string, upload *models.PhotoUpload) {
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		upload, err = transaction.GetPhotoUpload(userId, upload.ID)
		return err
	}); err != nil {
		self.writeProblem(response, err)
		return
	}
	self.writeOffsetConflict(response, upload)
}

// failAndWriteProblem answers an error from receiving or completing. A file
// that cannot be accepted ends its upload, so that its parts are deleted;
// an error of the server's own does not, so that the client can try again.
func (self *Store) failAndWriteProblem(ctx context.Context, response http.ResponseWriter, userId string, upload *models.PhotoUpload, err error) {
	var tooLarge *FileTooLargeError
	var overQuota *QuotaError
	// A refusal caused by other uploads' bytes is not this file's fault: it is
	// answered 503 with a Retry-After, and the upload stays, for the client to
	// resume once room is free.
	if errors.As(err, &overQuota) && overQuota.IsCausedByOtherUploads {
		self.writeProblem(response, err)
		return
	}
	if errors.As(err, &tooLarge) || errors.As(err, &overQuota) || errors.Is(err, ErrDigestMismatch) || errors.Is(err, ErrWrongSize) || errors.Is(err, ErrEmptyFile) || errors.Is(err, ErrNotAPhoto) || errors.Is(err, ErrPartDamaged) || errors.Is(err, ErrTooManyParts) {
		failContext := context.WithoutCancel(ctx)
		if failError := self.database.TransactionContext(failContext, func(transaction db.Transaction) error {
			_, err := transaction.FailPhotoUpload(userId, upload.ID)
			return err
		}); failError != nil {
			log.Warningf("cannot end upload %s: %s", upload.ID, failError)
		}
	}
	self.writeProblem(response, err)
}

// writeProblem answers an error with the status it deserves.
func (self *Store) writeProblem(response http.ResponseWriter, err error) {
	var tooLarge *FileTooLargeError
	var overQuota *QuotaError
	switch {
	case errors.As(err, &overQuota) && overQuota.IsCausedByOtherUploads:
		// Not a refusal of this file: every client takes a 503 with a
		// Retry-After as "try again", where a 413 is "this file is refused".
		response.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(overQuota.RetryAfter.Seconds()))))
		http.Error(response, err.Error(), http.StatusServiceUnavailable)
	case errors.As(err, &tooLarge), errors.As(err, &overQuota):
		http.Error(response, err.Error(), http.StatusRequestEntityTooLarge)
	case errors.Is(err, ErrDigestMismatch):
		http.Error(response, headerDeclaredSha256+": "+err.Error(), http.StatusBadRequest)
	case errors.Is(err, ErrWrongSize):
		http.Error(response, headerUploadLength+": "+err.Error(), http.StatusBadRequest)
	case errors.Is(err, ErrEmptyFile), errors.Is(err, ErrTooManyParts):
		http.Error(response, err.Error(), http.StatusBadRequest)
	case errors.Is(err, ErrPartDamaged):
		// The server's fault, not the client's: it is told to start again.
		http.Error(response, err.Error(), http.StatusInternalServerError)
	case errors.Is(err, ErrNotAPhoto):
		http.Error(response, err.Error(), http.StatusUnsupportedMediaType)
	case errors.Is(err, db.ErrNotFound):
		http.Error(response, "no such upload", http.StatusNotFound)
	case errors.Is(err, db.ErrTooMuchAsked):
		http.Error(response, "too many uploads are unfinished; finish or cancel some", http.StatusTooManyRequests)
	case errors.Is(err, db.ErrInvalidArguments):
		http.Error(response, "invalid upload", http.StatusBadRequest)
	default:
		log.Errorf("a photo upload failed: %s", err)
		http.Error(response, "the upload could not be stored", http.StatusInternalServerError)
	}
}

// absoluteUrl is a path on this server as a client reaches it.
func (self *Store) absoluteUrl(request *http.Request, path string) string {
	scheme := "http"
	if api.IsSecure(request, self.configuration().Server.TrustedProxies) {
		scheme = "https"
	}
	return scheme + "://" + request.Host + path
}
