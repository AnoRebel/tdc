-- Whether an image moves.
--
-- A thumbnail is a still frame, and a timeline shows the thumbnail where one
-- exists — so an animated GIF arrived frozen. Recorded rather than derived,
-- because deciding it means reading the file's structure, which is work the
-- server should do once at upload rather than on every read.
ALTER TABLE attachments ADD COLUMN animated BOOLEAN NOT NULL DEFAULT false;
