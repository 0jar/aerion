-- Aerion: rollback the v0.3.0 schema (migrations 31 + 32) back to v0.2.5 (v30).
--
-- This script reconstructs the v30 schema (`contacts` + `carddav_contacts` tables)
-- from the v32 unified schema (`contact_records` + `contact_emails` + sidecars)
-- via JOINs. No external backup file is needed — the unified schema IS the data;
-- the old shape is just a denormalized projection of it.
--
-- Aerion versions and the schemas they ship with:
--   - v0.2.5 (last released) → schema v30 (separate `contacts`, `carddav_contacts`)
--   - v0.3.0 (upcoming)      → schema v32 (unified `contact_records` + UUID identity)
--
-- v31 was an intermediate development schema that never shipped — no real-world
-- DB will ever be at v31 alone. The only rollback path that matters is v32 → v30
-- (the released-to-released transition).
--
-- Inherent data loss on rollback:
--   - Multi-field data (phones, addresses, URLs, IMPPs, org, title, note, bday,
--     nickname, categories) is dropped. v30's schema has no columns for these.
--   - The `vcard_raw` round-trip preservation is dropped (same reason).
--   - CardDAV record IDs are reshaped: each (record, email) pair becomes its own
--     row again, with synthetic IDs of the form `<record_id>:<email>`. Older
--     Aerion identifies contacts during sync by `href` (via GetContactByHref),
--     not by ID, so this works correctly — only the IDs differ.
--   - Local-record UUIDs are reduced back to email-keyed rows. Since v30's
--     `contacts` table was already keyed by email, this is the natural form
--     — the UUIDs were a v32-only concept.
--
-- USAGE
--   1. Quit Aerion completely.
--   2. Back up your aerion.db file just in case:
--        cp ~/.local/share/aerion/aerion.db ~/.local/share/aerion/aerion.db.bak
--      (or whatever your DB path is — `~/Library/Application Support/Aerion/`
--       on macOS, `%LOCALAPPDATA%\aerion\` on Windows).
--   3. Run this script against your DB:
--        sqlite3 ~/.local/share/aerion/aerion.db < rollback-v32-to-v30.sql
--   4. Launch the older Aerion (v0.2.5). It should start normally and your
--      contacts autocomplete should work.
--
-- If anything goes wrong, restore from the backup you made in step 2.

BEGIN TRANSACTION;

-- 1. Recreate legacy `contacts` table with v30 schema.
CREATE TABLE contacts (
    email TEXT PRIMARY KEY,
    display_name TEXT,
    send_count INTEGER DEFAULT 0,
    last_used DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    name_overridden INTEGER NOT NULL DEFAULT 0,
    kind TEXT NOT NULL DEFAULT 'collected'
);

CREATE INDEX idx_contacts_send_count ON contacts(send_count DESC);
CREATE INDEX idx_contacts_last_used ON contacts(last_used DESC);

-- 2. Restore local-contact rows. One row per (record, email) pair where the
--    record is sourced locally. Lossless: email/name/send_count/last_used/
--    name_overridden/kind all round-trip. The v32 record-id format (UUID) is
--    discarded — v30 keys by email, which is the natural identity for the
--    legacy schema.
INSERT INTO contacts (email, display_name, send_count, last_used, created_at, name_overridden, kind)
SELECT
    ce.email,
    cr.fn,
    ce.send_count,
    ce.last_used,
    cr.created_at,
    ce.name_overridden,
    COALESCE(cr.kind, 'collected')
FROM contact_records cr
JOIN contact_emails ce ON ce.record_id = cr.id
WHERE cr.source = 'local';

-- 3. Recreate legacy `carddav_contacts` table with v30 schema.
CREATE TABLE carddav_contacts (
    id TEXT PRIMARY KEY,
    addressbook_id TEXT NOT NULL,
    email TEXT NOT NULL,
    display_name TEXT,
    href TEXT,
    etag TEXT,
    synced_at DATETIME,
    FOREIGN KEY (addressbook_id) REFERENCES contact_source_addressbooks(id) ON DELETE CASCADE
);

CREATE INDEX idx_carddav_contacts_addressbook ON carddav_contacts(addressbook_id);
CREATE INDEX idx_carddav_contacts_email ON carddav_contacts(email);

-- 4. Restore carddav-contact rows. Re-introduces the fan-out: one row per
--    (record, email) pair. Synthetic ID `<record_id>:<email>` ensures uniqueness;
--    older Aerion matches contacts on next sync via (addressbook_id, href).
INSERT INTO carddav_contacts (id, addressbook_id, email, display_name, href, etag, synced_at)
SELECT
    cr.id || ':' || ce.email,
    crs.addressbook_id,
    ce.email,
    cr.fn,
    crs.href,
    crs.etag,
    crs.synced_at
FROM contact_records cr
JOIN contact_emails ce ON ce.record_id = cr.id
JOIN carddav_record_state crs ON crs.record_id = cr.id
WHERE cr.source = 'carddav';

-- 5. Drop the unified tables. Multi-field data is gone after this — same
--    semantics as removing columns from a v30 install that never had them.
DROP TABLE carddav_record_state;
DROP TABLE contact_categories;
DROP TABLE contact_impps;
DROP TABLE contact_urls;
DROP TABLE contact_addresses;
DROP TABLE contact_phones;
DROP TABLE contact_emails;
DROP TABLE contact_records;

-- 6. Roll back the migration tracker so older Aerion doesn't think v31/v32 have
--    been applied. After this, older Aerion sees schema_version=30 and starts
--    normally. The `>= 31` bound catches both v31 and v32 records — and any
--    future intermediate schemas, if one slipped in.
DELETE FROM migrations WHERE version >= 31;

COMMIT;

-- Verify (optional, run separately to confirm):
--   sqlite3 aerion.db "SELECT COUNT(*) FROM contacts; SELECT COUNT(*) FROM carddav_contacts; SELECT MAX(version) FROM migrations;"
