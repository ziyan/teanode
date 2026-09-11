import { useState } from 'react'

import { ChevronDownIcon, ChevronRightIcon } from './icons'
import { Select, SelectOption } from './select'
import { SettingsRow } from './settingsList'
import { Tag } from './common'
import { useTranslation } from '../i18n/i18n'

// A tool policy as an accordion: a family per row — the chevron, its name,
// how many tools, the word for the whole — and, opened, its tools indented
// under it, each with a word of its own. The operator's words are three
// (allowed, ask first, off); a person's are two. A family's word covers
// its tools unless a tool says otherwise.

export type PolicyTool = { name: string; family: string; description: string; confirms: boolean }

export function ToolPolicyAccordion({
  families,
  tools,
  options,
  policy,
  defaultWord,
  onChange,
}: {
  families: string[]
  tools: PolicyTool[]
  options: SelectOption[]
  policy: Record<string, string>
  // The word a name has when the policy says nothing about it.
  defaultWord: string
  onChange: (name: string, word: string) => void
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState<Record<string, boolean>>({})
  const wordFor = (name: string) => policy[name] ?? defaultWord
  const labelOf = (word: string) => options.find((option) => option.value === word)?.label ?? word

  return (
    <div className="tool-policy">
      {families.map((family) => {
        const members = tools.filter((tool) => tool.family === family)
        const familyWord = wordFor(family)
        return (
          <div className="tool-policy-family" key={family}>
            <div className="tool-policy-head">
              <button
                type="button"
                className="tool-policy-toggle"
                aria-expanded={!!open[family]}
                onClick={() => setOpen({ ...open, [family]: !open[family] })}
              >
                {open[family] ? <ChevronDownIcon size={14} /> : <ChevronRightIcon size={14} />}
                <strong>{family}</strong>
                <span className="muted">{t('agentSettings.familyTools', { count: String(members.length) })}</span>
              </button>
              <Select
                value={familyWord}
                label={`${family}: ${t('agentSettings.policy')}`}
                options={options}
                onChange={(value) => onChange(family, value)}
              />
            </div>
            {open[family] && (
              <div className="tool-policy-tools">
                {members.map((tool) => (
                  <SettingsRow
                    key={tool.name}
                    title={tool.name}
                    badge={tool.confirms ? <Tag value={t('agentSettings.asksByRisk')} /> : undefined}
                    subtitle={tool.description}
                    actions={
                      <Select
                        value={wordFor(tool.name)}
                        label={`${tool.name}: ${t('agentSettings.policy')}`}
                        options={
                          familyWord === defaultWord
                            ? options
                            : options.map((option) =>
                                option.value === defaultWord
                                  ? { ...option, label: t('agentSettings.policyInherits', { word: labelOf(familyWord) }) }
                                  : option,
                              )
                        }
                        onChange={(value) => onChange(tool.name, value)}
                      />
                    }
                  />
                ))}
                {members.length === 0 ? <p className="muted">{t('agentSettings.familyDynamic')}</p> : null}
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}
