// #4091: a search with square brackets, such as "[Grp] Some Show", found no torrents.
export default async function (t) {
  await t.stub.seed({ torrents: [
    { name: "[Grp] Some Show - 05 [1080p].mkv" },
    { name: "[Grp] Other Show - 01 [720p].mkv" },
    { name: "Made.Up.Movie.2024.1080p.WEB-GRP" },
  ] })
  await t.synced(3)
  await t.goto(`/instances/${t.instanceId}`)
  const rows = t.page.locator('[aria-label="Select row"]')
  await rows.nth(2).waitFor()
  const term = "[Grp] Some Show"
  const searched = t.page.waitForResponse(r => new URL(r.url()).searchParams.get("search") === term)
  await t.page.getByPlaceholder("Search torrents...").click()
  await t.page.keyboard.type(term)
  const { total } = await (await searched).json()
  await t.waitFor(async () => await rows.count() === total, "the list to show the search result")
  await t.shot("search [Grp] Some Show")
  t.assert.equal(await rows.count(), 1)
}
