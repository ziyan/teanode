package models

import "time"

// PhotoAlbumShareLevel is what somebody an album is shared with may do with
// it.
type PhotoAlbumShareLevel string

const (
	// PhotoAlbumShareLevelView sees the album's photos and saves them to a
	// library of their own.
	PhotoAlbumShareLevelView PhotoAlbumShareLevel = "view"
	// PhotoAlbumShareLevelContribute also adds photos of their own to the
	// album and takes out the ones they added.
	PhotoAlbumShareLevelContribute PhotoAlbumShareLevel = "contribute"
)

// IsValid says whether an album can be shared at this level.
func (self PhotoAlbumShareLevel) IsValid() bool {
	return self == PhotoAlbumShareLevelView || self == PhotoAlbumShareLevelContribute
}

// PhotoAlbumShare gives one person, or every member of one circle, a level of
// access to one album. Exactly one of GranteeUserID and GranteeCircleID is
// set.
type PhotoAlbumShare struct {
	ID           string `json:"id"`
	PhotoAlbumID string `json:"photoAlbumId"`

	GranteeUserID   string `json:"granteeUserId,omitempty"`
	GranteeCircleID string `json:"granteeCircleId,omitempty"`

	ShareLevel PhotoAlbumShareLevel `json:"shareLevel"`

	// IsLocationIncluded says the people reached see where the album's photos
	// were taken and receive the original files, which carry it.
	IsLocationIncluded bool `json:"isLocationIncluded"`

	CreatedAt time.Time `json:"createdAt"`

	// CreatedByUserID is empty once that account is gone.
	CreatedByUserID string `json:"createdByUserId,omitempty"`

	// GranteeUser, GranteeUsername, GranteeName and GranteeCircleName are
	// filled in by the listing, for a screen that says who an album is shared
	// with.
	GranteeUser       *User  `json:"-"`
	GranteeUsername   string `json:"granteeUsername,omitempty"`
	GranteeName       string `json:"granteeName,omitempty"`
	GranteeCircleName string `json:"granteeCircleName,omitempty"`
}

// PhotoAlbumAccess is what one person may do with one album, worked out by
// the database from who owns the album, the circles they are in and the
// shares that reach them. It is the one answer every door asks.
type PhotoAlbumAccess struct {
	PhotoAlbumID string

	// OwnerUserID is the person who owns the album, empty for a circle's
	// album, and CircleID the circle that owns it, empty for a person's.
	OwnerUserID string
	CircleID    string

	// IsOwner says the album is the person's own.
	IsOwner bool

	// ShareLevel is the level the person has: empty for the owner, who has
	// more than any level, PhotoAlbumShareLevelContribute for a member of the
	// circle that owns the album, and the highest of the shares that reach
	// them for another person's.
	ShareLevel PhotoAlbumShareLevel

	// IsManager says the person may rename, delete and share the album: its
	// owner, or an admin of the circle that owns it.
	IsManager bool

	// IsLocationIncluded says a share that reaches the person lets them see
	// where photos were taken. A circle album has no share, so its members see
	// only their own photos' places.
	IsLocationIncluded bool
}

// CanContribute says the person may add their own photos to the album.
func (self *PhotoAlbumAccess) CanContribute() bool {
	return self != nil && (self.IsOwner || self.ShareLevel == PhotoAlbumShareLevelContribute)
}

// PhotoAccess is why one person may read one photo that is not theirs.
type PhotoAccess struct {
	PhotoID     string
	OwnerUserID string

	// IsOwner says the photo is the person's own, and then nothing is held
	// back from them.
	IsOwner bool

	// IsLocationIncluded says an album that reaches the person lets them see
	// where this photo was taken and receive its original files.
	IsLocationIncluded bool
}
