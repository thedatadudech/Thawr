-- Schema version 2: what the client reported about itself (spec 007).
ALTER TABLE peers ADD COLUMN client_version TEXT NOT NULL DEFAULT '';
ALTER TABLE peers ADD COLUMN os TEXT NOT NULL DEFAULT '';
