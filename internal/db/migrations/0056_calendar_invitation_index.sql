-- An index nothing reads.
--
-- It was written for "is this message an invitation?", but the reader asks
-- that by the mailbox and the item -- which is the unique index beside it --
-- and nothing anywhere looks a row up by the message. Kept, it is a write on
-- every delivered message that carries a calendar part, for nobody.
DROP INDEX "calendar_invitation_mail";
