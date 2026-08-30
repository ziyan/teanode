# Read a pull request description and write its changelog entries, one file per
# kind, into outdir. Each entry is followed by the pull request number, so that
# a year later somebody reading the changelog can find the change itself.
#
#     awk -v number=42 -v outdir=/tmp/entries -f pull-request-entries.awk <body.md
#
# What counts as an entry: a bullet under a "### Added" (or Changed, and so on)
# heading, inside the "## Changelog" section of the description. Everything
# else in the description is for the reviewer, not the release notes.

function flush(    text) {
    text = entry
    sub(/[[:space:]]+$/, "", text)
    if (kind != "" && text != "" && text !~ /^[-*] *TODO:/) {
        # The number is left off when there is none: the same parser reads the
        # Unreleased section, whose entries were written straight into the file
        # and belong to no pull request.
        if (number == "") {
            printf "%s\n", text >> (outdir "/" kind)
        } else {
            printf "%s (#%s)\n", text, number >> (outdir "/" kind)
        }
    }
    entry = ""
}

BEGIN { inside = 0; kind = ""; entry = ""; comment = 0 }

# The template is mostly commentary, and the commentary explains the headings
# it does not want copied. Anything inside a comment is not an entry.
/<!--/ { comment = 1 }
comment { if (/-->/) { comment = 0 }; next }

# "## Changelog" opens the block; the next "## " heading closes it.
/^## / {
    flush()
    inside = (tolower($0) ~ /^## *changelog/)
    kind = ""
    next
}

!inside { next }

# A heading naming one kind. The template ships a placeholder listing all of
# them separated by pipes; that one names nothing and is ignored.
/^### / {
    flush()
    kind = ""
    if ($0 ~ /\|/) { next }
    if ($0 ~ /^### *(Added|Changed|Deprecated|Removed|Fixed|Security) *$/) {
        kind = $2
    }
    next
}

/^[-*] / { flush(); entry = $0; next }

# A blank line ends an entry; anything else while inside one is a wrapped
# line, and wrapped lines are what make a changelog readable.
/^[[:space:]]*$/ { flush(); next }
entry != "" { entry = entry "\n" $0 }

END { flush() }
