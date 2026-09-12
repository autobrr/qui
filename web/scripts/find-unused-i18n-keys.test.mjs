import test from "node:test"
import assert from "node:assert/strict"

import {
  collectKeyReferencesFromSource,
  collectLocaleKeys,
  findUnusedKeys,
  mergeReferences,
} from "./find-unused-i18n-keys.mjs"

function unusedKeysFor(bundles, sources) {
  return findUnusedKeys(
    collectLocaleKeys(bundles),
    mergeReferences(sources.map((source) => collectKeyReferencesFromSource(source, "src/example.tsx")))
  )
}

test("a longer key that shares a prefix does not keep the shorter one alive", () => {
  const bundles = {
    torrents: {
      detailsPanel: {
        banPeer: { title: "Ban Peer", description: "Ban this peer?" },
        banPeerPermanent: { title: "Ban Peer Permanently" },
      },
    },
  }

  const source = `
    const { t } = useTranslation("torrents")
    t("detailsPanel.banPeerPermanent.title")
  `

  assert.deepEqual(unusedKeysFor(bundles, [source]), [
    "torrents:detailsPanel.banPeer.description",
    "torrents:detailsPanel.banPeer.title",
  ])
})

test("identically named keys in other subtrees do not keep a dead subtree alive", () => {
  const bundles = {
    instances: {
      preferences: {
        shareLimit: { global: "Global", unlimited: "Unlimited" },
        workflowsOverview: {
          shareLimit: { global: "Global", unlimited: "Unlimited" },
        },
      },
    },
  }

  const source = `
    t("preferences.workflowsOverview.shareLimit.global")
    t("preferences.workflowsOverview.shareLimit.unlimited")
  `

  assert.deepEqual(unusedKeysFor(bundles, [source]), [
    "instances:preferences.shareLimit.global",
    "instances:preferences.shareLimit.unlimited",
  ])
})

test("a template literal prefix reaches every leaf below it", () => {
  const bundles = {
    common: {
      dateTime: {
        units: {
          second_one: "{{count}} second",
          second_other: "{{count}} seconds",
          minute_one: "{{count}} minute",
          minute_other: "{{count}} minutes",
        },
      },
    },
  }

  const source = "const label = translateCommon(`dateTime.units.${unit}`, { count: value })"

  assert.deepEqual(unusedKeysFor(bundles, [source]), [])
})

test("a template whose prefix is a variable falls back to matching the trailing segment", () => {
  const bundles = {
    instances: {
      preferences: {
        workflowsOverview: { summary: { dryRun: "Dry run", stale: "Stale" } },
      },
    },
  }

  const source = `
    const s = "preferences.workflowsOverview.summary"
    i18n.t(\`\${s}.dryRun\`, { ns })
  `

  assert.deepEqual(unusedKeysFor(bundles, [source]), [
    "instances:preferences.workflowsOverview.summary.stale",
  ])
})

test("two-leaf blocks are reported; there is no minimum subtree size", () => {
  const bundles = {
    automations: {
      queryBuilder: {
        capabilityReasons: { trackerHealth: "Needs 5.1+", localFilesystemAccess: "Needs access" },
      },
    },
  }

  assert.deepEqual(unusedKeysFor(bundles, ["const nothing = true"]), [
    "automations:queryBuilder.capabilityReasons.localFilesystemAccess",
    "automations:queryBuilder.capabilityReasons.trackerHealth",
  ])
})

test("keys reached through a namespace prefix, a labelKey field or a Trans attribute count as used", () => {
  const bundles = {
    common: { actions: { cancel: "Cancel" } },
    crossseed: { dirScan: { tagsDescription: "Tags" } },
    torrents: { fileTree: { name: "Name" } },
  }

  const sources = [
    `t("common:actions.cancel")`,
    `<Trans i18nKey="dirScan.tagsDescription" />`,
    `const columns = [{ labelKey: "fileTree.name" }]`,
  ]

  assert.deepEqual(unusedKeysFor(bundles, sources), [])
})

test("plural leaves are checked as one base key, but a pre-v4 _plural leaf is its own dead key", () => {
  const bundles = {
    instances: {
      preferences: {
        orphanScanOverview: {
          pathsIgnored_one: "{{count}} path ignored",
          pathsIgnored_other: "{{count}} paths ignored",
          pathsIgnored_plural: "{{count}} paths ignored",
        },
      },
    },
  }

  const source = `t("preferences.orphanScanOverview.pathsIgnored", { count })`

  assert.deepEqual(unusedKeysFor(bundles, [source]), [
    "instances:preferences.orphanScanOverview.pathsIgnored_plural",
  ])
})
