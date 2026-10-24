Scope note: these tasks implement the Location requirement of `messaging/realtime` and the location clause of `directory/profiles` Developer profiles. See `design.md` for how.

## 1. Server

- [x] 1.1 Migration 000443: `user_locations`, `person_location()`, directory locations moved to the account at no looser visibility, `directory_profiles.location` dropped; backup part; upgrade test
- [x] 1.2 `places` package: generator from GeoNames cities15000, committed gzipped list, `Nearest` with a 150 km limit; tests
- [x] 1.3 `GET/PUT /api/me/location`, `POST /api/me/location/detect`; position never stored or logged; documented routes and OpenAPI record regenerated
- [x] 1.4 Member list, directory profile, directory search and its location filter read the location through `person_location()`; viewer rank from one function; directory profile no longer holds or exports a location; moderation replaces the account location
- [x] 1.5 Test that a hidden or members-only location reaches nobody not entitled, across every response that returns a person, with the guard broken and the test seen to fail

## 2. Clients

- [x] 2.1 Types: `OwnLocation`, `location` on room members and the owner's profile response; removed from the profile document
- [x] 2.2 Settings section, loaded lazily: mode toggle, place, visibility, detect again, plain words when detection is refused or unavailable
- [x] 2.3 Background refresh: at most every few hours, only with standing permission, never prompts; unit tests
- [x] 2.4 Directory editor points to the setting; member rows show a location; English and Swahili; payload budget unchanged

## 3. Documentation

- [x] 3.1 User guide: Where you are, with a screenshot
- [x] 3.2 Hosting guide: the bundled list, its licence, nothing sent to a third party; NOTICE attribution
- [x] 3.3 Integration and directory guides: the endpoints and the retired profile field
