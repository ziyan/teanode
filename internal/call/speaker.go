package call

import (
	"time"
)

// Who is speaking. Each packet of a microphone says how loud its sound is
// (the audio level header extension, RFC 6464): from 0, the loudest, to 127,
// silence. Each person's share of packets that are speech is followed over
// about the last second, and the person with the largest share, when it is
// large enough, is the one speaking. A new speaker takes over only from one
// who has spoken a while or gone quiet, so two people talking over each other
// do not swap the featured picture every moment.

const (
	// speechLevelMost is the loudest a packet can be said to be (in -dBov)
	// and still be counted as silence: quieter than this is not speech.
	speechLevelMost = 50

	// speechWeight is how much each packet, 20 milliseconds of sound, moves
	// a person's share of speech: about the last second counts.
	speechWeight = 0.05

	// speakingShareLeast is the share of speech that makes a person the one
	// speaking.
	speakingShareLeast = 0.3

	// speakerHeldLeast is how long a person speaking stays so before somebody
	// else may take over while they still speak.
	speakerHeldLeast = 1500 * time.Millisecond

	// silentAfter is how long a person's microphone may send nothing before
	// their share fades: a muted device may stop sending.
	silentAfter = 300 * time.Millisecond

	// speakerTick is how often the room looks again at who is speaking.
	speakerTick = 250 * time.Millisecond
)

// voiceActivity is how much of what a person's microphone has lately sent
// was speech.
type voiceActivity struct {
	speechShare float64
	heardAt     time.Time
}

// hearLevel notes the loudness of one packet of a person's microphone.
func (self *room) hearLevel(userId string, level uint8, now time.Time) {
	self.voiceMutex.Lock()
	defer self.voiceMutex.Unlock()
	if self.voices == nil {
		self.voices = map[string]*voiceActivity{}
	}
	activity := self.voices[userId]
	if activity == nil {
		activity = &voiceActivity{}
		self.voices[userId] = activity
	}
	speech := 0.0
	if level <= speechLevelMost {
		speech = 1
	}
	activity.speechShare = activity.speechShare*(1-speechWeight) + speech*speechWeight
	activity.heardAt = now
}

// nextSpeaker is who is speaking now, given who was and since when.
func (self *room) nextSpeaker(current string, currentSince time.Time, now time.Time) string {
	self.voiceMutex.Lock()
	defer self.voiceMutex.Unlock()
	loudest, loudestShare := "", 0.0
	currentShare := 0.0
	for userId, activity := range self.voices {
		if now.Sub(activity.heardAt) > silentAfter {
			activity.speechShare /= 2
		}
		if userId == current {
			currentShare = activity.speechShare
		}
		if activity.speechShare > loudestShare {
			loudest, loudestShare = userId, activity.speechShare
		}
	}
	if loudest == "" || loudest == current || loudestShare < speakingShareLeast {
		return current
	}
	if current != "" && currentShare >= speakingShareLeast && now.Sub(currentSince) < speakerHeldLeast {
		return current
	}
	return loudest
}

// forgetVoice drops a person who left from who may be speaking.
func (self *room) forgetVoice(userId string) {
	self.voiceMutex.Lock()
	defer self.voiceMutex.Unlock()
	delete(self.voices, userId)
}

// followSpeakers looks at who is speaking every speakerTick until the room
// closes: a new speaker changes whose picture is sent whole and is told to
// the devices that asked. It also asks again for the pictures watchers still
// wait for.
func (self *room) followSpeakers() {
	ticker := time.NewTicker(speakerTick)
	defer ticker.Stop()
	for {
		select {
		case <-self.done:
			return
		case now := <-ticker.C:
			self.mutex.Lock()
			speaker := self.nextSpeaker(self.speakingUserId, self.speakerChangedAt, now)
			var told []*participant
			if speaker != self.speakingUserId {
				if self.speakingUserId != "" {
					self.previousSpeakingUserId = self.speakingUserId
				}
				self.speakingUserId, self.speakerChangedAt = speaker, now
				self.chooseLayers()
				for _, each := range self.participants {
					if each.shouldTellSpeaker {
						told = append(told, each)
					}
				}
			}
			videos := append([]*forwardedVideo(nil), self.videos...)
			self.mutex.Unlock()
			for _, each := range told {
				each.signal(Signal{SpeakingUserID: speaker})
			}
			for _, video := range videos {
				video.askPicturesStillWaited(now)
			}
		}
	}
}
