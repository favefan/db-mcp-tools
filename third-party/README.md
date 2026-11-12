# Supplemental third-party license texts

This directory contains license texts for adopted dependencies, not product implementations from reference projects. `notices.json` records package versions, retrieved sources, text hashes, and restrictions. Packaging verifies hashes and includes the texts in distribution manifests.

Prefer files supplied by each package. When a Cargo / npm package lacks a workspace license, retrieve the license at the same commit using Cargo VCS metadata or npm `gitHead`. The `selectors` source header explicitly uses MPL 2.0; the full Mozilla terms and exact source archive URL are included.

`react-remove-scroll-bar` 2.3.8 declares MIT but supplies no upstream license file, and its npm `gitHead` is unavailable in the declared repository. A standard SPDX MIT text and package author metadata are included with a review marker; no copyright notice is fabricated. No independent redistribution license has been found for the native DM module. Its status is recorded separately and is not covered by this project's MIT license.
