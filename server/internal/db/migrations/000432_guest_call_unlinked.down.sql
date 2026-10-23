ALTER TABLE users ADD CONSTRAINT users_guest_call_fkey FOREIGN KEY (guest_call) REFERENCES calls(id) ON DELETE SET NULL;
