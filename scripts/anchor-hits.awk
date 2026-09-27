# scripts/anchor-hits.awk —— 门禁第 16 行的判据本体（M299 取向的机器化）。
#
# 由 scripts/run-gates.sh 第 16 行调用：
#   gawk -v SRCLIST=<被引用文件清单> -f scripts/anchor-hits.awk docs/*.md
# 它**只报数不判红**（取向原话：「红了只报数不自动改」）——命中率跌了不拦交付，
# 因为拦下来也不会有人去逐条回改历史锚点（登记表约束 (1) 禁止改写已登记行），
# 那只会让整套门禁变成"大家都学会忽略的那一行"。真正会红的是**扫描面本身没成立**
# （一行 anchors= 都打不出来），那一格按 FAIL 记，理由同 B1：读空不读作通过。
#
# 输入：SRCLIST 指向一个"被引用文件的清单"（每行一个相对路径），主输入是若干文档。
# 对文档里每一处 `某文件.go:123` 形状的行号锚，问一个问题：
#   **同一行文档里用反引号引出的符号，是否落在被指的那个行上（±5 之内）？**
# 分四档：EXACT（就在该行）/ NEAR（±5）/ DRIFT（符号在该文件里、但离 claimed 行 >5）
#        / NOSYM（这一行文档没引出任何"存在于该文件"的符号 ⇒ 无法判定，不进分母）。
# 另两档是硬缺陷、单列不并进比率：MISSING（文件找不到——含指向 Wails/Go 标准库
# 与 x/sys 的**仓外锚**，那一类不是错，所以只报数不判）/ OOR（行号超出文件长度）。
#
# 口径偏松的两处，读数时要知道：任何 ≥6 字母的标识符形状都算候选词（常见英文词也参与匹配）
# ⇒ EXACT/NEAR **偏高**；同名文件取清单里第一个。所以这条只看趋势（这批比上批漂了多少），
# 不用来判定交付。

function load(f,   l, n, blob, b) {
	n = 0; blob = ""
	while ((getline l < f) > 0) { n++; lines[f SUBSEP n] = l; blob = blob l "\n" }
	cnt[f] = n; text[f] = blob; close(f)
	b = f; sub(/.*\//, "", b)
	if (!(b in firstof)) firstof[b] = f
}

BEGIN {
	while ((getline f < SRCLIST) > 0) load(f)
	for (k in cnt) nsrc++
}

{
	# 清空候选词表用 split("", a) 而不是 delete a：CI 的 ubuntu runner 上 awk 是 mawk，
	# 整数组 delete 是 gawk 扩展。写成一行人少踩一次"本地绿、CI 红"。
	split("", cand)
	s = $0
	while (match(s, /`[^`]+`/)) {
		tok = substr(s, RSTART + 1, RLENGTH - 2)
		s = substr(s, RSTART + RLENGTH)
		last = tok
		sub(/^.*\./, "", last)
		if (last ~ /^[A-Za-z_][A-Za-z0-9_]{5,}$/) cand[last] = 1
	}

	s = $0
	while (match(s, /[^ `|()（）\[\],"’‘]*\.(go|ts|tsx|vue|js|mjs|sh|json|yml|yaml|plist|mod|sum|py|c|h|md):[0-9]+/)) {
		a = substr(s, RSTART, RLENGTH)
		s = substr(s, RSTART + RLENGTH)
		p = a; sub(/:[0-9]+$/, "", p)
		ln = a; sub(/.*:/, "", ln); ln = ln + 0
		total++
		# 中文正文里没有空格分隔，路径捕获常把前面的句子一起吃进来
		# （实测 `有两处确认字面——ConfirmDialog.vue:147`）。⇒ 剥掉开头所有非路径字节。
		sub(/^[^A-Za-z0-9_.\/-]+/, "", p)
		b = p; sub(/.*\//, "", b)
		target = ""
		if (p in cnt) target = p
		else if (b in firstof) target = firstof[b]
		if (target == "" || cnt[target] == 0) { missing++; print "MISSING " p ":" ln; continue }
		if (ln > cnt[target]) { oor++; print "OOR " p ":" ln " (file has " cnt[target] " lines)"; continue }
		has = 0; hit = 0; near = 0
		for (c in cand) {
			if (index(text[target], c) == 0) continue
			has = 1
			if (index(lines[target SUBSEP ln], c) > 0) { hit = 1; break }
			lo = (ln - 5 > 1) ? ln - 5 : 1
			hi = (ln + 5 < cnt[target]) ? ln + 5 : cnt[target]
			for (k = lo; k <= hi; k++) if (index(lines[target SUBSEP k], c) > 0) { near = 1; break }
			if (near) break
		}
		if (!has) nosym++
		else if (hit) exact++
		else if (near) nearhit++
		else { drift++; print "DRIFT " p ":" ln }
	}
}

END {
	judged = exact + nearhit + drift
	rate = judged > 0 ? (100.0 * (exact + nearhit)) / judged : 0
	printf "anchors=%d src_files=%d EXACT=%d NEAR=%d DRIFT=%d NOSYM=%d MISSING=%d OOR=%d hit_rate=%.1f%%\n", \
		total, nsrc, exact, nearhit, drift, nosym, missing, oor, rate
}
