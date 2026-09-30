-- Going back to a release that reads the server secret only from the
-- database is refused while the database does not hold it: that release
-- would generate a new secret and lose everything sealed with this one. A
-- server that must go back restores the database backup taken before the
-- secret was moved out; docs/reference/deployment.md says more.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM "configuration"
        WHERE "key" = 'server' AND "value" ~ '(^|\n)secretCheck: ' AND "value" !~ '(^|\n)secret: [^"\n]'
    ) THEN
        RAISE EXCEPTION 'the server secret is kept in a file, not in the database; an older release would generate a new one';
    END IF;
END
$$;
