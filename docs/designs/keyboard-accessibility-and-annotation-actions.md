# Design: keyboard accessibility and annotation card actions

## Status

Proposed for the accessibility milestone. This design covers keyboard shortcut
discovery, panel and comment commands, and separating replies from annotation
lifecycle actions. It does not change annotation storage, lifecycle rules, or
HTTP mutation semantics.

## Problem

The primary review workflow currently requires repeated pointer travel between
the document and two sidebars. The document and annotation panels have native
buttons, but there is no fast keyboard path to toggle them or start a comment,
and there is no in-product reference for keyboard commands.

Annotation cards also put the reply form inside the nested `Actions`
disclosure. Replying is an ordinary, frequent conversation action; lifecycle
changes, reattachment, and resolution metadata are less frequent operations.
Hiding both behind one label makes replies unnecessarily difficult to find and
makes `Actions` describe more than the content it reveals.

The requested Space-prefixed commands and `?` are character-only shortcuts.
That includes a sequence such as Space followed by E. WCAG 2.2 success
criterion 2.1.4 requires users to be able to turn character-key shortcuts off,
remap them to include a non-printable modifier, or limit them to the focused
component. The implementation therefore cannot add global character shortcuts
without also adding a persistent off mechanism.

## Goals

- Make all shortcuts discoverable from `?` and from a permanently reachable
  top-bar control.
- Toggle the documents sidebar with Space, then E.
- Toggle the review sidebar with Space, then R.
- Open the new-comment form with Space, then C.
- Let users disable all global character-key shortcuts and retain that choice.
- Preserve native typing, form controls, text composition, browser commands,
  and assistive-technology interaction.
- Keep every shortcut as an accelerator for a visible, keyboard-operable
  control rather than the only way to perform an operation.
- Put visible `Reply` and `Actions` controls in each expanded annotation card.
- Make `Actions` reveal only lifecycle, reattachment, and resolution controls;
  make `Reply` reveal only the reply form.
- Keep reply and action controls together in the existing annotation card,
  with predictable focus order and visible focus treatment.

## Non-goals

- Arbitrary user-defined key remapping in the first slice.
- Shortcuts for every annotation lifecycle transition.
- Changing status transition permissions, server routes, optimistic
  concurrency, thread entry kinds, or sidecar schemas.
- Making a hidden sidebar a modal drawer on desktop.
- Replacing ordinary Tab, Shift+Tab, Enter, Space, arrow-key, or Escape
  behavior within existing controls.
- Adding a command palette.

## Required user experience

### Shortcut reference

The top bar gains a `Keyboard shortcuts` button. It is present in both viewer
and review modes, remains reachable when either sidebar is hidden, and opens
the same reference as `?`.

The reference is a modal dialog titled `Keyboard shortcuts`. It lists only
commands available in the current mode and uses semantic groups rather than a
visual-only key chart:

| Keys | Command | Availability |
| --- | --- | --- |
| `?` | Open keyboard shortcuts | Viewer and review modes |
| `Space`, then `E` | Toggle documents sidebar | Viewer and review modes |
| `Space`, then `R` | Toggle review sidebar | Review mode only |
| `Space`, then `C` | Add a new comment | Review mode only |
| `Escape` | Close keyboard shortcuts | While the dialog is open |

Each key is rendered with `<kbd>`. The table or definition list has ordinary
text equivalents so screen readers announce both the keys and their purpose.
The dialog also contains an `Enable global shortcuts` checkbox and a visible
`Close` button.

Opening the dialog moves focus inside it. Tab and Shift+Tab remain inside the
dialog, Escape closes it, and closing restores focus to the top-bar button or
the element that invoked it. The page behind a modal dialog is visually
obscured and inert. Prefer the native `<dialog>` element and `showModal()`;
apply `aria-labelledby` to its visible heading and do not add `aria-modal`
unless the implementation actually prevents background interaction.

The shortcut preference is stored as
`code-annotator.global-shortcuts-enabled` in `localStorage`, because the
accessibility choice should survive navigation, new tabs, and later sessions.
If storage is unavailable, the control still changes the current page's
in-memory setting. The first-run default is enabled. Turning shortcuts off
takes effect immediately, cancels any pending Space sequence, and disables
`?` as well; the top-bar button remains available so shortcuts can be enabled
again.

### Space leader behavior

When global shortcuts are enabled, an unmodified Space press outside an
editable or interactive context starts a short-lived leader sequence. The
viewer prevents the normal Space scroll for that press and waits 1,000 ms for
the second key. E, R, or C executes the matching available command. Escape,
an unsupported key, loss of document visibility, or timeout cancels the
sequence without executing a command.

This deliberately changes bare Space behavior while shortcuts are enabled and
must be stated in the shortcut dialog: `Space starts a shortcut and does not
scroll the page while global shortcuts are enabled.` PageDown, Shift+PageDown,
arrow keys, scrollbars, and pointer scrolling remain available. Users who rely
on Space for page scrolling can disable global shortcuts. We should not
silently emulate native Space scrolling after a timeout; browser scrolling and
assistive-technology behavior are not safe to reconstruct in application code.

While waiting for the second key, a small non-modal status message announces
`Shortcut: Space …` through an existing-style polite status region. Completion
announces the result, such as `Documents sidebar hidden`; cancellation by
timeout stays visually quiet and is not announced repeatedly.

Shortcut matching uses `KeyboardEvent.key` so it follows the user's active
keyboard layout. It is case-insensitive for E, R, and C. It ignores repeated
keydown events, dead keys, `isComposing`, and any event with Ctrl, Alt, Meta, or
Shift other than the Shift needed by the current layout to produce `?`.

Global shortcuts do not start or complete when the event target is:

- `input`, `textarea`, `select`, or `button`;
- a link, summary, or element with an interactive ARIA role;
- a contenteditable element or a descendant of one; or
- inside the shortcuts dialog.

The same suppression applies for the complete sequence. For example, moving
focus into a textarea after Space cancels the pending leader rather than
interpreting the next typed letter as a command. Native controls keep their
standard Space and Enter behavior.

### Panel commands

Shortcuts invoke the existing `.documents-toggle` and `.review-toggle`
buttons instead of directly editing classes, `hidden`, ARIA attributes, or
storage. There remains one panel-toggle implementation and one persisted panel
preference for pointer and keyboard use.

Toggling a sidebar normally leaves focus where it is. If the command hides the
sidebar that currently contains focus, focus first moves to that sidebar's
visible top-bar toggle so focus is never left in hidden content. The toggle's
`aria-expanded`, visible label, panel `hidden` state, layout class, and existing
session preference remain synchronized.

Space, then R is unavailable outside review mode. Unavailable contextual
commands do not appear in the shortcut dialog and do not reserve their second
key.

### Add-comment command

Space, then C performs the same action as the visible `Add comment` button. In
review mode it:

1. opens the review sidebar if it is hidden;
2. opens the existing new-comment form if it is hidden; and
3. moves focus to the Comment textarea, using `preventScroll` only when that
   does not hide the focused control outside the visible review-panel area.

Existing selected-text state is retained, so the form continues to offer the
captured selection when the shortcut is used. If the form is already open,
the command only focuses its Comment textarea. The shortcut does not submit,
clear, or change the comment scope.

The visible `Add comment` button remains the authoritative trigger and keeps
`aria-controls="annotation-form"` and `aria-expanded` synchronized. The
shortcut calls that behavior rather than duplicating it.

### Annotation card controls

An expanded annotation card keeps its source, original comment, attribution,
and thread in the current reading order. Immediately after that content it
renders one action bar:

```text
[Reply] [Actions] [Close, when valid]

Reply panel (when expanded)
  Reply as …
  Reply …
  Add reply

Actions panel (when expanded)
  Reattach selection, when available
  Lifecycle action, role, message/summary, commit, and Update status
```

`Reply` is a real button controlling a reply region in the same card. It has a
stable `aria-controls` target and an accurate `aria-expanded` value. Opening it
moves focus to the reply textarea so a reviewer can type immediately. Closing
it while focus is inside the reply region returns focus to `Reply`; closing it
from the button leaves focus on the button. Reply submission continues to use
the existing reply endpoint, review token, sidecar revision, validation, and
HTMX panel replacement.

`Actions` is a sibling disclosure button with its own stable `aria-controls`
target and `aria-expanded` state. It reveals only reattachment and lifecycle
forms. Opening it leaves focus on the button; the next Tab reaches the first
available action control. This follows the WAI-ARIA disclosure convention in
which Enter and Space activate a button and `aria-expanded` communicates its
state.

Reply and Actions may both be expanded. They are not tabs and must not be
implemented as an exclusive tablist, because the user may need to refer to a
draft reply while examining a lifecycle action. Their revealed regions appear
in DOM order immediately after their controls, and CSS must not visually
reorder them away from that focus order.

The compact quick `Close` action remains visible when `applied -> closed` is
valid. It is a direct action, not a disclosure, and is not duplicated inside
the lifecycle form. Existing accessible names that distinguish one card's
Close button from another remain required.

Escape pressed from within an open Reply or Actions region collapses only that
region and returns focus to its controlling button. Escape does not collapse
the outer annotation card. Enter and Space on either disclosure button use
native button behavior; no custom key simulation is added.

After a reply or action request replaces `#annotation-panel-content`, focus
must not fall back to `<body>`. The adapter records the annotation ID,
operation kind, and invoking control before the request. On success it focuses
the updated card summary or the returned status associated with that operation;
on a 409 or 422 response it restores the relevant disclosure, preserves the
server-returned values, and focuses the first invalid field or operation status.
If the annotation is no longer present, focus moves to the annotation panel
heading. Focus restoration uses stable server-rendered IDs rather than list
indexes.

## Technical design

### Server-rendered HTML and view models

`web/templates/page.html` adds the top-bar shortcuts button, modal dialog, and
one polite shortcut status region. The server decides which command rows are
rendered from review-mode availability; JavaScript does not construct the
shortcut list.

`web/templates/annotation-actions.html` is split into semantic sibling pieces:

- the action bar with Reply, Actions, and optional quick Close buttons;
- a reply region containing the existing `.annotation-reply` form; and
- an actions region containing `.annotation-reattach` and
  `.annotation-lifecycle` forms, but no reply form.

The regions can remain in one template or become small named templates. Each
annotation needs stable IDs derived from the already validated annotation ID,
for example:

```text
annotation-reply-panel-<annotation-id>
annotation-actions-panel-<annotation-id>
annotation-reply-status-<annotation-id>
annotation-lifecycle-status-<annotation-id>
```

`internal/server/review_views.go` adds those IDs to the annotation actions view
model. It continues to calculate valid transitions and `CanQuickClose`; no
lifecycle decisions move into TypeScript.

Use native `<button type="button">` elements and `hidden` controlled regions.
Do not place focusable descendants inside a `<summary>`, add positive
`tabindex`, or use a clickable `div`. The outer annotation `<details>` remains
the card-level disclosure and source-navigation control.

### Browser modules

Add a focused `web/src/keyboard-shortcuts.ts` module and initialize it from
`viewer.ts`. It owns:

- preference loading and updates;
- editable/interactive-target detection;
- the Space leader finite-state machine and timeout cleanup;
- `?` dialog opening and modal focus behavior;
- contextual command lookup; and
- dispatch to existing visible controls.

It does not own panel layout or review-form state. Panel commands call the
corresponding native button. Add-comment dispatches one named document event,
`code-annotator:add-comment`, handled by the current review panel controller;
this avoids importing the asynchronous review composition root into the viewer
module. The handler opens the current HTMX-replaced sidebar and form through
their existing controllers.

Extend `viewer-layout.ts` with a small public command path for safe panel
toggling and focus relocation when a panel containing focus is hidden. Pointer
clicks and shortcut activation must converge on the same `handlePanelToggle`
function.

Add an `annotation-card-disclosures.ts` controller or keep the equivalent
focused logic in `review.ts`. It binds by event delegation on the current
review panel so HTMX replacements do not accumulate handlers. It owns only
Reply/Actions expanded state, Escape behavior, and operation-focus capture and
restoration. `review-fragments.ts` remains responsible for configuring fields
from server-supplied lifecycle behavior.

All listeners use the existing cleanup/`AbortController` lifecycle. Pending
leader timers are cleared on cleanup, page visibility loss, navigation, and
shortcut disablement.

### Styling

`web/src/styles/_review.scss` changes the action bar to wrap without changing
DOM order. Reply and Actions buttons share the existing compact control
treatment but retain a clearly visible `:focus-visible` indicator in both
themes and at high zoom. Revealed panels receive a visual boundary and heading
or accessible label so users can distinguish conversation from state changes.

The shortcuts dialog must work at 320 CSS pixels and 400% zoom without
horizontal page scrolling. Its command list may stack key and description
instead of forcing a wide table. `prefers-reduced-motion: reduce` disables
nonessential opening or status animations.

Generated JavaScript and CSS continue to be produced by `npm run build:web`;
generated assets are not edited by hand.

## Accessibility requirements

- All functionality remains operable through visible native controls when
  global shortcuts are disabled.
- Character-only shortcuts have an immediately available off mechanism, as
  required by WCAG 2.2 SC 2.1.4.
- The shortcuts modal follows the WAI-ARIA modal dialog keyboard and focus
  pattern.
- Reply and Actions follow the disclosure pattern: native button activation,
  `aria-expanded`, and stable `aria-controls` relationships.
- Focus never remains inside content made `hidden`, and DOM/focus order follows
  the visible card order.
- Every keyboard-operable control has a visible, non-obscured focus indicator
  in light and dark themes.
- Polite status messages announce command results and mutation outcomes without
  announcing leader timeout or scroll-position changes.
- Shortcut labels do not depend on color, icons, or punctuation alone.
- Commands are suppressed during text entry, IME composition, and interaction
  with native or ARIA widgets.
- Existing keyboard selection, annotation-to-source navigation, panel resize,
  document search `/`, and native disclosure behavior remain intact.

Primary guidance:

- [WCAG 2.2: Character Key Shortcuts](https://www.w3.org/WAI/WCAG22/Understanding/character-key-shortcuts)
- [WAI-ARIA APG: Modal Dialog Pattern](https://www.w3.org/WAI/ARIA/apg/patterns/dialog-modal/)
- [WAI-ARIA APG: Disclosure Pattern](https://www.w3.org/WAI/ARIA/apg/patterns/disclosure/)
- [WCAG 2.2: Focus Order](https://www.w3.org/WAI/WCAG22/Understanding/focus-order.html)
- [WCAG 2.2: Focus Visible](https://www.w3.org/WAI/WCAG22/Understanding/focus-visible)

## Verification plan

### Unit and template tests

- Test shortcut matching, case normalization, leader timeout, cancellation,
  repeated keys, modifiers, composition, and disabled preference.
- Test suppression for every editable and interactive target category,
  including descendants of contenteditable and ARIA widgets.
- Test that panel commands activate the existing toggle path and relocate focus
  before hiding a focused sidebar.
- Test add-comment behavior with the review panel hidden, form hidden, form
  already open, and an existing text selection.
- Test modal open/close focus restoration and Tab wrapping.
- Test Reply and Actions independently and simultaneously expanded, their ARIA
  state, textarea focus, Escape behavior, and HTMX replacement restoration.
- Extend Go template/view tests to prove the reply form is outside the actions
  region, Actions contains only valid server-supplied operations, IDs are
  stable and unique, and quick Close is not duplicated.

### Browser tests

- Complete every command with only the keyboard in File and Changes views.
- Verify `?`, the top-bar button, modal focus containment, Escape, restored
  focus, and the persistent enable/disable setting.
- Verify Space does not scroll while enabled and resumes native scrolling when
  shortcuts are disabled.
- Verify typing `?`, spaces, E, R, and C in every form field causes no command.
- Toggle each sidebar in both directions and assert `hidden`, layout class,
  button text, `aria-expanded`, focus, and saved preference agree.
- Invoke Add comment from a collapsed review layout and verify the textarea is
  visible and focused without losing the current source selection.
- Open Reply without opening Actions, open Actions without opening Reply, open
  both, submit each form, exercise 409 and 422 responses, and verify focus does
  not fall to the document body.
- Repeat at the responsive breakpoint, 320 CSS pixels, 200% and 400% zoom,
  both themes, and reduced motion.
- Run a keyboard-only and screen-reader smoke pass on macOS VoiceOver plus one
  Chromium accessibility-tree inspection before approval.

## Must-do acceptance criteria

- `?` and the always-visible top-bar button open one accurate shortcut
  reference.
- Space then E toggles the documents sidebar; Space then R toggles the review
  sidebar; Space then C opens and focuses the new-comment form.
- Users can turn all global character shortcuts off and later turn them back
  on without using a shortcut.
- No global shortcut fires while typing, composing text, or operating an
  interactive control.
- Hiding a sidebar never strands focus inside hidden content, and every command
  has an equivalent visible native control.
- Every expanded annotation card shows separate Reply and Actions controls in
  one action bar.
- Reply reveals only the reply form. Actions reveals only reattachment and
  lifecycle operations. Both can be open without invalid nested interaction.
- Existing reply, reattach, lifecycle, quick-close, concurrency, selection,
  and HTMX behavior remains correct.
- Automated unit, Go template, and browser coverage passes, followed by the
  manual keyboard and screen-reader checks above.

## Implementation sequence

1. Add server-rendered shortcut help, stable annotation-region IDs, and split
   Reply/Actions markup with Go template tests.
2. Add the shortcut preference, modal controller, leader state machine, and
   panel commands with TypeScript unit tests.
3. Connect Add comment to the review controller and add independent annotation
   disclosures plus focus restoration.
4. Update responsive/focus styling, rebuild generated assets, and add browser
   coverage.
5. Run the complete Go and web suites, perform keyboard/screen-reader checks,
   and update this status plus `project_status.md` when the implementation is
   approved.
