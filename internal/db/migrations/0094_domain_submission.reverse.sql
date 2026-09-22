-- Stop senders and discard pending retry requests before downgrade. Accepted mail
-- remains, but older binaries cannot recognize these submission identities.
DROP TABLE "domain_submission";
