package models

import "time"

// PhotoFace is one face found in a photo. The box is in fractions of the
// picture from its top left corner, so it means the same on a preview as on
// the original. The vector the face was grouped by is never sent to a client.
type PhotoFace struct {
	ID        string    `json:"id"`
	PhotoID   string    `json:"photoId"`
	UserID    string    `json:"userId"`
	CreatedAt time.Time `json:"createdAt"`

	BoxLeftFraction   float64 `json:"boxLeftFraction"`
	BoxTopFraction    float64 `json:"boxTopFraction"`
	BoxWidthFraction  float64 `json:"boxWidthFraction"`
	BoxHeightFraction float64 `json:"boxHeightFraction"`

	// DetectionScore is how sure the detector was that this is a face, from 0
	// to 1.
	DetectionScore float64 `json:"detectionScore"`

	// FaceVector is 128 numbers of unit length, kept to compare faces by.
	FaceVector []float32 `json:"-"`

	// PhotoFaceGroupID is the group the face is in, empty for a face too
	// small or too uncertain to be grouped. IsGroupConfirmed says the person
	// put it there, or took it out of another group, so it is not grouped
	// again.
	PhotoFaceGroupID string `json:"photoFaceGroupId"`
	IsGroupConfirmed bool   `json:"isGroupConfirmed"`

	// ContactID and ContactName are those of the face's group, empty while
	// the group is unnamed. FacePictureURL is where the face's picture is
	// fetched from. All three are worked out when the face is read.
	// IsOwnContact is set by whoever answers the caller: it says the group's
	// contact is the caller's own (User.ContactID), so the face is them.
	ContactID      string `json:"contactId" gorm:"-"`
	ContactName    string `json:"contactName" gorm:"-"`
	FacePictureURL string `json:"facePictureUrl" gorm:"-"`
	IsOwnContact   bool   `json:"isOwnContact" gorm:"-"`
}

// PhotoFaceGroup is the faces the server believes are one person. It is
// nobody until the person names it by choosing a contact.
type PhotoFaceGroup struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// ContactID is the contact that names the group, and ContactAddressBookID
	// the address book it is in (a contact is only unique inside its book).
	// Both are empty while the group is unnamed.
	ContactID            string `json:"contactId"`
	ContactAddressBookID string `json:"contactAddressBookId"`

	// ContactName, FaceCount, PhotoCount and FacePictureURL are worked out
	// when the group is read, from the photos that are not in the trash: a
	// face in a trashed photo is not counted until the photo is restored.
	// FacePictureURL is the picture of the group's cover face.
	ContactName    string `json:"contactName"`
	FaceCount      int    `json:"faceCount"`
	PhotoCount     int    `json:"photoCount"`
	FacePictureURL string `json:"facePictureUrl"`

	IsHidden bool `json:"isHidden"`

	// IsOwnContact says the contact that names the group is the caller's own
	// (User.ContactID), so the group is the caller themselves. It is set by
	// whoever answers the caller, and is false for a group nobody named.
	IsOwnContact bool `json:"isOwnContact"`

	// CoverPhotoFaceID is the face the group is shown by; the id of
	// FacePictureURL.
	CoverPhotoFaceID string `json:"coverPhotoFaceId"`

	// AccountUserID and Account say who the group's contact is on this server,
	// when the caller linked it to somebody in their circles and still shares
	// a circle with them. Both are null otherwise. They are the caller's own
	// note and are set by whoever answers the caller.
	AccountUserID *string                `json:"accountUserId"`
	Account       *PhotoFaceGroupAccount `json:"account"`
}

// PhotoFaceGroupAccount is the person a group's contact is linked to, as the
// circles show them.
type PhotoFaceGroupAccount struct {
	UserID           string  `json:"userId"`
	Username         string  `json:"username"`
	Name             string  `json:"name"`
	AvatarURL        *string `json:"avatarUrl"`
	AvatarColorIndex int     `json:"avatarColorIndex"`
}
