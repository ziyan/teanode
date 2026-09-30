-- Changes no table. It marks the release that can keep the server secret in
-- a file (--secret-file) and out of the configuration table. An older
-- release that found such a database would see no secret, generate a new
-- one, and could no longer open anything sealed with the old one; this row
-- is what makes an older release refuse to start here instead.
SELECT 1;
