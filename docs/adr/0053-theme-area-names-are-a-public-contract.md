---
status: accepted
date: 2026-11-14
---

# Theme area names are a public contract

Custom themes style the main areas of qui through the `data-qui-area` attribute. Users save these themes on their own servers, and qui cannot update them. A renamed or removed name breaks those themes with no error.

The names live in one list in the web app. A change can add a name. A rename or a removal needs a note in the release notes. Class names and the structure inside an area are not part of the contract.
