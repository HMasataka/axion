CREATE TABLE settings (
    key             TEXT PRIMARY KEY,
    value           TEXT NOT NULL,
    updated_at      INTEGER NOT NULL
);

INSERT INTO settings(key, value, updated_at) VALUES
  ('ignore_list', '[".git",".DS_Store","Thumbs.db","*.tmp","*.swp","*~"]', strftime('%s','now')*1000000000),
  ('blob_gc_age_seconds', '604800', strftime('%s','now')*1000000000),
  ('max_file_size_bytes', '1073741824', strftime('%s','now')*1000000000);
