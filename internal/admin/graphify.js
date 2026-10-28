(function () {
  "use strict";
  const esc = (s) =>
    String(s == null ? "" : s).replace(
      /[&<>"']/g,
      (c) =>
        ({
          "&": "&amp;",
          "<": "&lt;",
          ">": "&gt;",
          '"': "&quot;",
          "'": "&#39;",
        })[c],
    );
  const num = (n) => Number(n || 0).toLocaleString();
  const tok = (n) =>
    n >= 1e6
      ? (n / 1e6).toFixed(1) + "M"
      : n >= 1e3
        ? (n / 1e3).toFixed(1) + "k"
        : String(Math.round(n || 0));
  const pct = (a, b) => (b > 0 ? Math.round((100 * a) / b) + "%" : "-");
  const avg = (n) => n.toFixed(1);
  const when = (iso) => {
    const d = new Date(iso);
    if (isNaN(d)) return "";
    return (
      d.toLocaleDateString(undefined, { day: "numeric", month: "short" }) +
      " " +
      d.toLocaleTimeString(undefined, {
        hour12: false,
        hour: "2-digit",
        minute: "2-digit",
      })
    );
  };
  const age = (iso) => {
    const h = (Date.now() - new Date(iso).getTime()) / 36e5;
    if (!isFinite(h)) return "-";
    return h < 1
      ? "under an hour ago"
      : h < 48
        ? Math.round(h) + " h ago"
        : Math.round(h / 24) + " days ago";
  };

  const tile = (k, v, sub, title) =>
    `<div class="tile"${title ? ` title="${esc(title)}"` : ""}><div class="k">${esc(k)}</div><div class="v">${v}</div>` +
    (sub
      ? `<div class="sub" style="color:var(--faint);font-size:11.5px;margin-top:2px">${sub}</div>`
      : "") +
    `</div>`;

  function verdict(r) {
    const t = r.totals;
    const notes = [];
    if (!r.setup.binary) {
      notes.push([
        "warn",
        "The graphify command is not on this Mac's PATH, so Claude cannot call it.",
      ]);
    }
    if (t.lookups === 0 && t.graph_sessions === 0) {
      notes.push([
        "info",
        "No session in this window ran in a repository with a graph, and none called graphify.",
      ]);
    } else if (t.lookups === 0) {
      notes.push([
        "bad",
        `Not used: ${num(t.graph_sessions)} sessions ran in a repository with a graph and none asked it a question.`,
      ]);
    } else {
      const share =
        t.graph_sessions > 0
          ? Math.round((100 * t.sessions_using) / t.graph_sessions)
          : 0;
      notes.push([
        share >= 50 ? "ok" : "warn",
        `Asked ${num(t.lookups)} times across ${num(t.sessions_using)} of ${num(t.graph_sessions)} sessions in repositories with a graph (${share}%).`,
      ]);
    }
    if (r.setup.binary && !r.setup.hook_installed) {
      notes.push([
        "info",
        "The Claude Code hook that points Claude at the graph before it searches (graphify claude install) is not in settings.json, so use depends on CLAUDE.md and the skill alone.",
      ]);
    }
    return notes
      .map(([c, m]) => `<div class="note ${c}">${esc(m)}</div>`)
      .join("");
  }

  function tiles(r) {
    const t = r.totals;
    const returned = t.result_tokens;
    const cited = t.cited_tokens;
    return (
      `<div class="tiles">` +
      tile(
        "Questions asked",
        num(t.lookups),
        `${num(t.queries)} query · ${num(t.explains)} explain · ${num(t.paths)} path`,
      ) +
      tile(
        "Sessions using it",
        pct(t.sessions_using, t.graph_sessions),
        `${num(t.sessions_using)} of ${num(t.graph_sessions)} with a graph · ${num(t.sessions)} in all`,
        "Sessions that asked at least one question, out of sessions that ran in a repository with graphify-out/graph.json",
      ) +
      tile(
        "Answer size",
        tok(returned),
        t.lookups ? `~${tok(returned / t.lookups)} per question` : "",
        "Estimated tokens graphify's answers put into context (characters / 4)",
      ) +
      tile(
        "Files cited",
        tok(cited),
        "if each were read once",
        "Estimated tokens of the files the answers pointed at, each capped at about one Read (10k tokens). A ceiling on what the answers replaced: a careful Grep and a short Read costs far less.",
      ) +
      tile(
        "Cut short",
        pct(t.truncated, t.lookups),
        `${num(t.truncated)} answers hit the token budget`,
        "Answers graphify truncated: the part Claude needed may have been cut",
      ) +
      tile(
        "Graph rebuilds",
        num(t.updates),
        "graphify update",
        "Runs of graphify update. Code-only updates cost no tokens.",
      ) +
      `</div>`
    );
  }

  function bars(r) {
    const max = Math.max(1, ...r.daily.map((d) => d.lookups));
    const cols = r.daily
      .map((d) => {
        const h = Math.round((100 * d.lookups) / max);
        return (
          `<div title="${esc(d.date)}: ${d.lookups} questions, ${d.updates} rebuilds" style="flex:1;min-width:3px;display:flex;flex-direction:column;justify-content:flex-end;height:100%">` +
          `<div style="height:${h}%;min-height:${d.lookups ? 2 : 0}px;background:var(--accent,#3b82f6);border-radius:2px 2px 0 0"></div></div>`
        );
      })
      .join("");
    const first = r.daily[0],
      last = r.daily[r.daily.length - 1];
    return (
      `<div style="padding:14px 16px 4px"><h3 style="margin:0 0 8px;font-size:12px;color:var(--faint);font-weight:600">Questions per day</h3>` +
      `<div style="display:flex;gap:2px;height:70px;align-items:flex-end">${cols}</div>` +
      `<div style="display:flex;justify-content:space-between;font-size:11px;color:var(--faint);margin-top:4px"><span>${esc(first ? first.date : "")}</span><span>peak ${max} a day</span><span>${esc(last ? last.date : "")}</span></div></div>`
    );
  }

  function repos(r) {
    if (!r.repos.length) return "";
    const nw = `style="white-space:nowrap"`;
    const rows = r.repos.map((x) => {
      const behind = x.commits_since > 0 ? `<span style="color:var(--warn)">${x.commits_since} ${x.commits_since === 1 ? "commit" : "commits"} behind</span>` : x.commits_since === 0 ? `<span style="color:var(--faint)">current</span>` : "";
      const graph = !x.has_graph ? `<span style="color:var(--faint)">none</span>` :
        [esc(age(x.graph_built)), behind, x.graph_nodes ? `<span style="color:var(--faint)">${num(x.graph_nodes)} nodes</span>` : ""].filter(Boolean).join(" · ");
      const cut = x.lookups ? pct(x.truncated, x.lookups) : "-";
      return `<tr><td class="nm" title="${esc(x.path || "")}">${esc(x.repo)}</td><td ${nw}>${graph}</td>` +
        `<td class="n" ${nw}>${num(x.sessions_using)} / ${num(x.sessions)}</td><td class="n">${num(x.lookups)}</td><td class="n">${num(x.updates)}</td>` +
        `<td class="n">${cut}</td><td class="n">${tok(x.result_tokens)}</td><td class="n">${tok(x.cited_tokens)}</td><td ${nw}>${x.last ? esc(when(x.last)) : "-"}</td></tr>`;
    }).join("");
    return `<div style="padding:14px 16px 4px"><h3 style="margin:0 0 8px;font-size:12px;color:var(--faint);font-weight:600">By repository</h3>` +
      `<div class="stbl-wrap"><table class="stbl"><thead><tr><th>Repository</th><th>Graph</th><th class="n">Sessions using</th><th class="n">Questions</th><th class="n">Rebuilds</th>` +
      `<th class="n" title="Share of answers cut at the token budget">Cut short</th><th class="n" title="Estimated tokens in answers">Answers</th><th class="n" title="Estimated tokens of cited files, each capped at about one Read">Cited files</th><th>Last</th></tr></thead><tbody>${rows}</tbody></table></div></div>`;
  }

  function compare(r) {
    const c = r.compare;
    if (!c.using_sessions || !c.other_sessions) return "";
    return `<div style="padding:14px 16px 4px"><h3 style="margin:0 0 8px;font-size:12px;color:var(--faint);font-weight:600">What Claude did instead</h3>` +
      `<div class="note info" style="margin-top:0">In repositories with a graph, sessions that asked it questions made ${avg(c.using_explore)} Read, Grep and Glob calls on average (${num(c.using_sessions)} sessions), against ${avg(c.other_explore)} for sessions that did not (${num(c.other_sessions)}). ` +
      `This is an observation, not a test: sessions that ask questions tend to be longer and harder, so the figures say nothing about cause.</div></div>`;
  }

  function recent(r) {
    if (!r.recent.length) return "";
    const rows = r.recent
      .map((c) => {
        const size = c.nodes
          ? `${num(c.shown || c.nodes)}${c.shown && c.shown < c.nodes ? " of " + num(c.nodes) : ""} nodes`
          : "";
        const flag = c.failed
          ? `<span style="color:var(--bad)">failed</span>`
          : c.truncated
            ? `<span style="color:var(--warn)">cut short</span>`
            : "";
        return (
          `<tr><td style="white-space:nowrap">${esc(when(c.at))}</td><td>${esc(c.repo)}</td><td>${esc(c.kind)}</td>` +
          `<td style="overflow-wrap:anywhere" title="${esc(c.text)}">${esc(c.text)}</td><td class="n">${size}</td><td class="n">${c.result_tokens && c.kind !== "update" && c.kind !== "skill" ? tok(c.result_tokens) : ""}</td><td>${flag}</td></tr>`
        );
      })
      .join("");
    return (
      `<div style="padding:14px 16px 12px"><h3 style="margin:0 0 8px;font-size:12px;color:var(--faint);font-weight:600">Latest calls</h3>` +
      `<div class="stbl-wrap"><table class="stbl"><thead><tr><th>When</th><th>Repository</th><th>Call</th><th>Question</th><th class="n">Result</th><th class="n">Tokens</th><th></th></tr></thead><tbody>${rows}</tbody></table></div></div>`
    );
  }

  const method =
    `<div style="padding:4px 16px 14px;font-size:12px;color:var(--faint);line-height:1.55">` +
    `Counted from Claude Code's transcripts in <code>~/.claude/projects</code>: every <code>graphify query</code>, <code>explain</code> and <code>path</code> run, with the size of its answer. ` +
    `Token figures are characters / 4. Whether the questions saved work is not something a transcript can prove: an independent test found graphify beat opening whole files by 83% but was level with careful search and short reads, and its own 71x figure is one best-case corpus. ` +
    `Read "Files cited" as a ceiling, "Cut short" as a quality signal, and the comparison above as a hint.</div>`;

  function render(el, r) {
    el.innerHTML =
      `<div style="padding:0 16px">${verdict(r)}</div>` +
      tiles(r) +
      bars(r) +
      repos(r) +
      compare(r) +
      recent(r) +
      method;
  }

  window.BurstGraphify = {
    mount(el, sel) {
      let busy = false;
      const load = async () => {
        if (busy) return;
        busy = true;
        try {
          const res = await fetch(
            "/api/graphify?days=" + encodeURIComponent(sel.value),
          );
          if (!res.ok) throw new Error((await res.text()).trim() || res.status);
          render(el, await res.json());
        } catch (e) {
          el.innerHTML = `<div class="note bad" style="margin:12px 16px">${esc(e.message || e)}</div>`;
        } finally {
          busy = false;
        }
      };
      sel.addEventListener("change", load);
      load();
      return { reload: load };
    },
  };
})();
