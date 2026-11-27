-- The goals the forward file wrote, found by the id it made them with, the
-- title and the built in writer. A goal the person has since reworded is
-- theirs, written by them, and stays. The settings columns the job read were
-- never dropped, so the day and time the person chose are still there.
DELETE FROM "agent_conversation"
WHERE "kind" = 'goal' AND "goal_written_by" = 'built_in' AND "goal_title" = 'Look around'
    AND "id" = 'la' || substr(md5('look-around:' || "agent_id"), 1, 24);
