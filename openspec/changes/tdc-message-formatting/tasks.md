Scope note: these tasks implement the "Message formatting" requirement of `messaging/channels` and the "Composing formatted messages" requirement of `clients/web`. The canonical specs were edited directly.

## 1. Rendering

- [x] 1.1 Render the Markdown subset with the existing parser in its own instance: raw HTML, images, tables, rules, indented code, setext headings, link definitions and bare-URL autolinks switched off; headings as a bold line; links only to http, https and mailto; DOMPurify limited to the subset's tags; verify with XSS attempts, entities, nested emphasis and code fences
- [x] 1.2 Run the existing link detection over what is left, never inside code; mentions and channel names unchanged; a single paragraph rendered inline so plain messages look as before; memoise per body
- [x] 1.3 Code blocks: language label, copy button, horizontal scrolling inside the bubble at every width, highlighting loaded on demand with colours that hold contrast in both themes
- [x] 1.4 "Show formatting in messages" preference, on by default, beside motion and density

## 2. Plain text

- [x] 2.1 One set of stripping rules in the server, the web layer and the desktop application, tested against one shared case file
- [x] 2.2 Strip for notification previews, push payloads, desktop banners and the search index; mentions inside code do not notify
- [x] 2.3 Strip in the interface wherever a message is quoted as text; link previews ignore links in code

## 3. Composer

- [x] 3.1 Shortcuts that wrap and unwrap the selection, listed in the keyboard reference, clear of the desktop menu's keys
- [x] 3.2 Enter in an open fence starts a line; editing in place keeps lines
- [x] 3.3 Optional toolbar, remembered per browser, with a syntax reference

## 4. Documentation and checks

- [x] 4.1 User guide formatting section; migration guide; integration guide on what bots should send
- [x] 4.2 Payload budget not raised; browser check of rendering in light and dark, at desktop and phone widths, and of the shortcuts
