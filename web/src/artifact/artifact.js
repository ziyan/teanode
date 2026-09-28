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
  // The series colours, in order, after the dashboard's own chart of usage:
  // the leaf from the mark first, then quieter colours that sit beside it
  // rather than shout over it. Readable on white, and lifted on dark.
  const light = ['#729d39', '#6f7f96', '#d97706', '#3f8f8a', '#b4533c', '#8a6fb0', '#a9c47f', '#9a9aa0']
  const lifted = ['#9fca63', '#9aa8bd', '#fbbf24', '#6cc4bd', '#e08a72', '#b39ddb', '#c6dd9f', '#7c7c84']
  const fontFamily = "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif"

  const theme = () => {
    const text = token('--text') || (dark() ? '#f4f4f5' : '#18181b')
    const muted = token('--muted') || (dark() ? '#a0a0a8' : '#6f6f76')
    const border = token('--border') || (dark() ? '#2a2a2e' : '#e7e7e4')
    const surface = token('--surface') || (dark() ? '#1a1a1d' : '#ffffff')
    const leaf = token('--leaf') || (dark() ? lifted[0] : light[0])
    const palette = [leaf].concat((dark() ? lifted : light).slice(1))
    // Labels at the dashboard's axis size, in its muted colour. ECharts
    // outlines a label by default, which on a dark page smears every digit
    // into the next.
    const small = { color: muted, fontFamily, fontSize: 11, textBorderWidth: 0 }
    // Gridlines, and nothing else drawn for an axis: no ticks, and no line
    // down the side of the values, as in the dashboard's usage chart.
    const axis = {
      axisLine: { show: false, lineStyle: { color: border } },
      axisTick: { show: false },
      axisLabel: Object.assign({ margin: 10 }, small),
      nameTextStyle: Object.assign({ align: 'left' }, small),
      splitLine: { lineStyle: { color: border, width: 1 } },
      splitArea: { show: false },
    }
    const categoryAxis = Object.assign({}, axis, {
      axisLine: { show: true, lineStyle: { color: border } },
      splitLine: { show: false },
    })
    // A value over a bar or a point says what the gridlines only suggest,
    // and where two would overlap, as they do in a narrow drawer, one of
    // them is dropped rather than written through the other.
    const valueLabel = Object.assign({ fontWeight: 500 }, small, { color: text })
    return {
      color: palette,
      backgroundColor: 'transparent',
      textStyle: { color: text, fontFamily },
      // The title at the left and the legend at the right, so the two
      // never sit on each other; the plot below both, with room for its
      // labels.
      title: {
        left: 0,
        textStyle: { color: text, fontFamily, fontWeight: 600, fontSize: 14 },
        subtextStyle: { color: muted, fontFamily, fontSize: 12 },
      },
      legend: {
        right: 0,
        top: 2,
        icon: 'roundRect',
        itemWidth: 10,
        itemHeight: 10,
        itemGap: 16,
        textStyle: { color: muted, fontFamily, fontSize: 12 },
        pageTextStyle: { color: muted },
      },
      grid: { left: 4, right: 4, top: 48, bottom: 4, containLabel: true },
      tooltip: {
        backgroundColor: surface,
        borderColor: border,
        borderWidth: 1,
        padding: [8, 12],
        textStyle: { color: text, fontFamily, fontSize: 12 },
        extraCssText: 'box-shadow: 0 8px 28px rgba(0, 0, 0, 0.22); border-radius: 6px;',
        axisPointer: {
          lineStyle: { color: border },
          crossStyle: { color: border },
          shadowStyle: { color: dark() ? 'rgba(255, 255, 255, 0.04)' : 'rgba(0, 0, 0, 0.04)' },
        },
      },
      categoryAxis,
      valueAxis: axis,
      timeAxis: categoryAxis,
      logAxis: axis,
      line: {
        smooth: false,
        symbol: 'circle',
        symbolSize: 5,
        showSymbol: false,
        lineStyle: { width: 2 },
        label: valueLabel,
        labelLayout: { hideOverlap: true },
      },
      bar: {
        itemStyle: { borderRadius: [3, 3, 0, 0] },
        barMaxWidth: 32,
        barCategoryGap: '40%',
        label: valueLabel,
        labelLayout: { hideOverlap: true },
      },
      scatter: { symbolSize: 8, label: valueLabel, labelLayout: { hideOverlap: true } },
      pie: {
        itemStyle: { borderColor: surface, borderWidth: 2, borderRadius: 3 },
        label: { color: text, fontFamily, fontSize: 12 },
        labelLine: { lineStyle: { color: border } },
      },
      dataZoom: { textStyle: { color: muted } },
      visualMap: { textStyle: { color: muted } },
    }
  }

  // The theme leaves 48 pixels above the plot, which is room for a title and
  // a legend on one line. A subtext goes on a second line and had nowhere to
  // be: it landed on the topmost axis label, so "last six months" was written
  // through the 200. The plot moves down when there is one, and only then,
  // because a chart without a subtext should not pay for the space.
  //
  // Only when the option has not said where the plot goes. A chart that sets
  // its own grid means it.
  const roomForTheTitle = (option) => {
    if (!option || !option.title || !option.title.subtext) return option
    const grid = option.grid
    // A chart may have several grids, and then grid is an array. Copying
    // an array with Object.assign gives an object with numeric keys, which
    // is not a grid at all and draws nothing -- so each one is widened on
    // its own.
    if (Array.isArray(grid)) {
      if (grid.some((one) => one && one.top !== undefined)) return option
      return Object.assign({}, option, {
        grid: grid.map((one) => Object.assign({}, one, { top: 68 })),
      })
    }
    if (grid && grid.top !== undefined) return option
    return Object.assign({}, option, { grid: Object.assign({}, grid, { top: 68 }) })
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
    drawn.setOption(roomForTheTitle(option))
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
