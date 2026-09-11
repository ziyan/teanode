// The agent's page and the dashboard agree on a look. Served at
// /assets/artifact.js, after /assets/echarts.min.js: it applies the theme
// the drawer asked for (#theme=dark or #theme=light in the address, else
// the system's), registers the dashboard's palette as an ECharts theme, and
// gives the page teanode.chart(element, option), which draws a chart in
// that theme, fitting its box and following it when the box resizes.
;(() => {
  const root = document.documentElement
  const asked = /theme=(dark|light)/.exec(window.location.hash || '')
  if (asked) root.setAttribute('data-theme', asked[1])

  const token = (name) => getComputedStyle(root).getPropertyValue(name).trim()
  const dark = () => {
    const chosen = root.getAttribute('data-theme')
    return chosen ? chosen === 'dark' : window.matchMedia('(prefers-color-scheme: dark)').matches
  }
  // The series colours, in order: readable on white, and lifted on dark.
  const light = ['#2f6db5', '#d97706', '#16a34a', '#dc2626', '#7c3aed', '#0891b2', '#be185d', '#4d7c0f']
  const lifted = ['#6ea8e8', '#fbbf24', '#4ade80', '#f87171', '#a78bfa', '#22d3ee', '#f472b6', '#a3e635']
  const fontFamily = "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif"

  const theme = () => {
    const text = token('--text') || (dark() ? '#f4f4f5' : '#18181b')
    const muted = token('--muted') || (dark() ? '#a0a0a8' : '#6f6f76')
    const border = token('--border') || (dark() ? '#2a2a2e' : '#e7e7e4')
    const surface = token('--surface') || (dark() ? '#1a1a1d' : '#ffffff')
    const axis = {
      axisLine: { lineStyle: { color: border } },
      axisTick: { lineStyle: { color: border } },
      axisLabel: { color: muted, fontFamily },
      nameTextStyle: { color: muted, fontFamily },
      splitLine: { lineStyle: { color: border } },
      splitArea: { areaStyle: { color: ['transparent', 'transparent'] } },
    }
    return {
      color: dark() ? lifted : light,
      backgroundColor: 'transparent',
      textStyle: { color: text, fontFamily },
      // The title at the left and the legend at the right, so the two
      // never sit on each other; the plot below both, with room for its
      // labels.
      title: { left: 0, textStyle: { color: text, fontFamily, fontWeight: 600, fontSize: 14 }, subtextStyle: { color: muted, fontFamily } },
      legend: { right: 0, top: 2, textStyle: { color: muted, fontFamily }, pageTextStyle: { color: muted } },
      grid: { left: 8, right: 8, top: 48, bottom: 8, containLabel: true },
      tooltip: {
        backgroundColor: surface,
        borderColor: border,
        borderWidth: 1,
        textStyle: { color: text, fontFamily },
        extraCssText: 'box-shadow: 0 4px 16px rgba(0, 0, 0, 0.12); border-radius: 8px;',
      },
      categoryAxis: axis,
      valueAxis: axis,
      timeAxis: axis,
      logAxis: axis,
      line: { smooth: false, symbolSize: 6, lineStyle: { width: 2 } },
      bar: { itemStyle: { borderRadius: [3, 3, 0, 0] }, barMaxWidth: 48 },
      pie: { itemStyle: { borderColor: surface, borderWidth: 2 }, label: { color: text, fontFamily } },
      dataZoom: { textStyle: { color: muted } },
      visualMap: { textStyle: { color: muted } },
    }
  }

  const drawnCharts = new Set()
  window.addEventListener('resize', () => drawnCharts.forEach((drawn) => drawn.resize()))
  const chart = (element, option) => {
    const target = typeof element === 'string' ? document.querySelector(element) : element
    if (!target) throw new Error('teanode.chart: no element to draw in')
    if (!window.echarts) throw new Error('teanode.chart: put <script src="/assets/echarts.min.js"></script> before /assets/artifact.js')
    window.echarts.registerTheme('teanode', theme())
    // A box drawn into again starts over, rather than keeping a chart
    // nobody can see under the new one.
    const previous = window.echarts.getInstanceByDom(target)
    if (previous) {
      drawnCharts.delete(previous)
      previous.dispose()
    }
    const drawn = window.echarts.init(target, 'teanode', { renderer: 'svg' })
    drawn.setOption(option)
    drawnCharts.add(drawn)
    return drawn
  }

  // The drawer cannot see how tall the page is from outside its sandbox,
  // so the page says: once drawn, and again whenever it changes.
  const tell = () => {
    if (window.parent === window) return
    window.parent.postMessage({ teanodeArtifact: { height: document.documentElement.scrollHeight } }, '*')
  }
  window.addEventListener('load', tell)
  if (window.ResizeObserver) new ResizeObserver(tell).observe(document.documentElement)

  window.teanode = { chart, theme, dark }
})()
