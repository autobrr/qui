// #4090: with a category "anime", typing "anime.x" in Set Category sent category=".x".
export default async function (t) {
  await t.stub.seed({ categories: ["anime", "movies"], torrents: [{ name: "Made.Up.Show.S01E01.1080p.WEB-GRP" }] })
  await t.synced()
  await t.goto(`/instances/${t.instanceId}`)
  await t.page.locator('[aria-label="Select row"]').first().click()
  await t.page.locator("[role=toolbar] button:has(svg.lucide-folder)").click()
  await t.page.locator("#newCategory").pressSequentially("anime.x")
  await t.shot("typed anime.x")
  await t.page.getByRole("dialog").getByRole("button", { name: "Set Category" }).click()
  const [call] = await t.waitFor(async () => {
    const calls = await t.stub.calls("/api/v2/torrents/setCategory")
    return calls.length && calls
  }, "qui to send setCategory")
  t.assert.equal(call.form.category, "anime.x")
}
