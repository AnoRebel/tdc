package events

/*
TypeMessageUpdated records a program replacing a message's text and controls
in place.

Distinct from an edit, which a person makes to their own words and which
carries only a body. An update carries the controls too, and a version: it is
how a program answers a press by changing the question everybody sees, and the
version is what lets a press made through the old view be refused rather than
counted against the new one.
*/
const TypeMessageUpdated Type = "message.updated"
