import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { graphql } from '../../api'
import { uploadFiles } from '../../upload'
import { FinancePaystub, TaxFormView, TaxYearFigures, TaxYearSummary } from './financeApi'
import { FinanceTaxesSection, offeredYears, taxDocumentPolling } from './financeTaxes'

const toast = vi.hoisted(() => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }))
vi.mock('../../api', () => ({ graphql: vi.fn(), askAgentAbout: vi.fn() }))
vi.mock('../../i18n/i18n', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string>) => (values ? `${key} ${JSON.stringify(values)}` : key),
    plural: (count: number, forms: { one: string; other: string }, values?: Record<string, string>) =>
      `${count === 1 ? forms.one : forms.other}${values ? ` ${JSON.stringify(values)}` : ''}`,
    language: 'en',
  }),
}))
vi.mock('../../components/toast', () => ({ useToast: () => toast }))
vi.mock('../../upload', () => ({ uploadFiles: vi.fn() }))
const execute = vi.mocked(graphql)
const upload = vi.mocked(uploadFiles)

const savedPolling = { ...taxDocumentPolling }
beforeEach(() => {
  taxDocumentPolling.intervalMS = 5
  taxDocumentPolling.attemptCount = 20
})

afterEach(() => {
  cleanup()
  execute.mockReset()
  upload.mockReset()
  toast.done.mockReset()
  toast.failed.mockReset()
  toast.failure.mockReset()
  Object.assign(taxDocumentPolling, savedPolling)
})

// Figures for an invented employer that pays 4,000.00 every two weeks:
// 300.00 to a traditional 401(k) and 100.00 to an HSA, so federal wages
// are 3,600.00 and Social Security and Medicare wages 3,700.00 a
// paycheck; ten paychecks so far, sixteen more projected.
function figures(paycheckCount: number): TaxYearFigures {
  const times = (amount: number) => (amount * paycheckCount).toFixed(2)
  return {
    wagesAmount: times(3600),
    federalIncomeTaxAmount: times(500),
    socialSecurityWagesAmount: times(3700),
    socialSecurityTaxAmount: times(229.4),
    medicareWagesAmount: times(3700),
    medicareTaxAmount: times(53.65),
    dependentCareAmount: '0.00',
    retirementTraditionalAmount: times(300),
    retirementRothAmount: '0.00',
    hsaAmount: times(100),
    healthCoverageAmount: '0.00',
    localIncomeTaxAmount: '0.00',
    taxYearStateFigures: [{ stateCode: 'CA', stateWagesAmount: times(3600), stateIncomeTaxAmount: times(96.95) }],
  }
}

// An invented paystub that adds up: 4,000.00 less 880.00 of taxes and
// 400.00 of deductions is 2,720.00.
const octoberPaystub: FinancePaystub = {
  id: 'paystub-october',
  agentAttachmentId: 'attachment-october',
  employerName: 'Example Employer LLC',
  employeeName: 'Pat Example',
  payDate: '2026-10-02',
  periodStartOn: '2026-09-14',
  periodEndOn: '2026-09-27',
  currencyCode: 'USD',
  grossAmount: '4000.0000',
  yearToDateGrossAmount: '40000.0000',
  totalTaxAmount: '880.0000',
  totalDeductionAmount: '400.0000',
  netAmount: '2720.0000',
  paystubCheckState: 'balanced',
  checkDifferenceAmount: '0',
  createdAt: '2026-10-02T12:00:00Z',
  modifiedAt: '2026-10-02T12:00:00Z',
  paystubLines: [
    {
      id: 'line-regular',
      lineNumber: 1,
      paystubLineKind: 'earning',
      description: 'Regular',
      hours: '80',
      rateAmount: '50.0000',
      currentAmount: '4000.0000',
      yearToDateAmount: '40000.0000',
    },
    {
      id: 'line-federal',
      lineNumber: 2,
      paystubLineKind: 'tax',
      description: 'Federal Income Tax',
      paystubTaxKind: 'federal_income',
      currentAmount: '500.0000',
      yearToDateAmount: '5000.0000',
      currentWagesAmount: '3600.0000',
      yearToDateWagesAmount: '36000.0000',
    },
    {
      id: 'line-social-security',
      lineNumber: 3,
      paystubLineKind: 'tax',
      description: 'Social Security',
      paystubTaxKind: 'social_security',
      currentAmount: '229.4000',
      yearToDateAmount: '2294.0000',
    },
    {
      id: 'line-medicare',
      lineNumber: 4,
      paystubLineKind: 'tax',
      description: 'Medicare',
      paystubTaxKind: 'medicare',
      currentAmount: '53.6500',
      yearToDateAmount: '536.5000',
    },
    {
      id: 'line-state',
      lineNumber: 5,
      paystubLineKind: 'tax',
      description: 'State Income Tax',
      paystubTaxKind: 'state_income',
      taxJurisdictionCode: 'CA',
      currentAmount: '96.9500',
      yearToDateAmount: '969.5000',
    },
    {
      id: 'line-retirement',
      lineNumber: 6,
      paystubLineKind: 'deduction',
      description: '401(k)',
      paystubDeductionKind: 'retirement_traditional',
      currentAmount: '300.0000',
      yearToDateAmount: '3000.0000',
    },
    {
      id: 'line-hsa',
      lineNumber: 7,
      paystubLineKind: 'deduction',
      description: 'HSA',
      paystubDeductionKind: 'hsa',
      currentAmount: '100.0000',
      yearToDateAmount: '1000.0000',
      employerCurrentAmount: '25.0000',
      employerYearToDateAmount: '250.0000',
    },
  ],
}

// The paystub two weeks before, which the finding below is about and
// which is not on the page listed.
const septemberPaystub: FinancePaystub = {
  ...octoberPaystub,
  id: 'paystub-september',
  agentAttachmentId: 'attachment-september',
  employerName: 'Example Employer LLC',
  payDate: '2026-09-18',
}

// An invented W-2 from a second invented employer for a second invented
// person, whose Social Security agrees with its wages.
const sampleWorksW2: TaxFormView = {
  financeTaxForm: {
    id: 'tax-form-sample',
    agentAttachmentId: 'attachment-w2',
    taxFormKind: 'w2',
    taxYear: 2026,
    isCorrected: false,
    payerName: 'Sample Works Inc',
    recipientName: 'Sam Example',
    taxFormEntries: [
      { id: 'entry-1', entryNumber: 1, boxCode: '1', amount: '50000.0000', isChecked: false },
      { id: 'entry-12', entryNumber: 2, boxCode: '12', boxQualifier: 'D', amount: '2000.0000', isChecked: false },
    ],
    createdAt: '2026-10-02T12:00:00Z',
    modifiedAt: '2026-10-02T12:00:00Z',
  },
  taxFormFindings: [
    {
      taxFormFindingCode: 'social_security_rate',
      isAgreeing: true,
      expectedAmount: '3224.00',
      printedAmount: '3224.00',
      findingText: 'Social Security tax is 6.2% of Social Security wages.',
    },
  ],
}

const yearSummary: TaxYearSummary = {
  taxYear: 2026,
  socialSecurityWageBaseAmount: '184500.00',
  isYearOver: false,
  paystubCount: 2,
  taxFormCount: 1,
  figures: figures(10),
  projectedFigures: figures(26),
  taxYearEmployments: [
    {
      employeeName: 'Pat Example',
      employerName: 'Example Employer LLC',
      taxYearFigureSource: 'paystubs',
      latestPayDate: '2026-10-02',
      paystubCount: 10,
      payCadenceDayCount: 14,
      isPaystubYearComplete: false,
      remainingPayCount: 16,
      figures: figures(10),
      paystubFigures: figures(10),
      projectedFigures: figures(26),
      taxYearComparisons: [],
    },
    {
      employeeName: 'Sam Example',
      employerName: 'Sample Works Inc',
      taxYearFigureSource: 'w2',
      taxFormId: 'tax-form-sample',
      latestPayDate: '2026-12-25',
      paystubCount: 26,
      isPaystubYearComplete: true,
      remainingPayCount: 0,
      figures: figures(10),
      taxYearComparisons: [
        {
          boxCode: '1',
          boxLabel: 'Wages, tips, other compensation',
          w2Amount: '36000.00',
          paystubAmount: '36000.00',
          differenceAmount: '0.00',
          isMatching: true,
        },
        {
          boxCode: '2',
          boxLabel: 'Federal income tax withheld',
          w2Amount: '5000.00',
          paystubAmount: '4988.00',
          differenceAmount: '12.00',
          isMatching: false,
        },
      ],
    },
  ],
  taxYearPeople: [
    { employeeName: 'Pat Example', excessSocialSecurityTaxAmount: '0.00', figures: figures(10) },
    { employeeName: 'Sam Example', excessSocialSecurityTaxAmount: '0.00', figures: figures(10) },
  ],
  taxYearFindings: [
    {
      taxYearFindingCode: 'pay_date_gap',
      employeeName: 'Pat Example',
      employerName: 'Example Employer LLC',
      findingText: 'Four weeks pass between the paystubs of September 4 and October 2; one in between may be missing.',
      paystubIds: ['paystub-september'],
    },
  ],
}

// answer plays the server: the document says which operation is asked.
function answer({
  summary = () => yearSummary,
  paystubs = () => [octoberPaystub],
  taxForms = () => [sampleWorksW2],
}: {
  summary?: () => TaxYearSummary
  paystubs?: () => FinancePaystub[]
  taxForms?: () => TaxFormView[]
} = {}) {
  execute.mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('TaxYear(')) return { TaxYear: summary() }
    if (document.includes('FinancePaystubs(')) {
      const listed = paystubs()
      const offset = Number(variables?.offset ?? 0)
      const limit = Number(variables?.limit ?? 60)
      return {
        FinancePaystubs: {
          financePaystubs: listed.slice(offset, offset + limit),
          hasMore: offset + limit < listed.length,
          totalCount: listed.length,
        },
      }
    }
    if (document.includes('FinancePaystub(')) {
      return {
        FinancePaystub: [octoberPaystub, septemberPaystub].find((paystub) => paystub.id === variables?.paystubId),
      }
    }
    if (document.includes('FinanceTaxForms(')) {
      const listed = taxForms()
      return { FinanceTaxForms: { taxForms: listed, hasMore: false, totalCount: listed.length } }
    }
    if (document.includes('ReadTaxDocument(')) {
      return { ReadTaxDocument: { agentAttachmentIds: [variables?.agentAttachmentId], agentJobIds: ['job-invented'] } }
    }
    if (document.includes('DeletePaystub(')) return { DeletePaystub: true }
    if (document.includes('DeleteTaxForm(')) return { DeleteTaxForm: true }
    return {}
  })
}

function calledWith(operation: string) {
  return execute.mock.calls.filter(([document]) => document.includes(`${operation}(`)).map(([, variables]) => variables)
}

let shownSearch = ''
function SearchProbe() {
  shownSearch = useLocation().search
  return null
}

function renderSection(address = '/finance/taxes?year=2026') {
  render(
    <MemoryRouter initialEntries={[address]}>
      <FinanceTaxesSection />
      <SearchProbe />
    </MemoryRouter>,
  )
}

// panel is the panel a heading heads.
function panel(heading: string): HTMLElement {
  return screen.getByRole('heading', { name: heading }).closest('section')!
}

describe('the Taxes section', () => {
  it('puts the year together: a projection to December 31 and the W-2 against the paystubs', async () => {
    answer()
    renderSection()
    const projected = await waitFor(() =>
      panel('finance.taxEmploymentTitle {"employee":"Pat Example","employer":"Example Employer LLC"}'),
    )
    expect(calledWith('TaxYear')).toContainEqual({ taxYear: 2026 })
    // Where the figures come from, from how many paystubs through which day.
    expect(within(projected).getByText(/^finance\.taxFromPaystubsOther .*"count":"10"/)).toBeTruthy()
    // So far and by December 31, side by side, with how many paychecks.
    expect(within(projected).getByText('finance.taxByDecember31')).toBeTruthy()
    const wagesRow = within(projected).getByText('finance.taxWages').closest('tr')!
    expect(within(wagesRow).getByText('$36,000.00')).toBeTruthy()
    expect(within(wagesRow).getByText('$93,600.00')).toBeTruthy()
    expect(within(projected).getByText(/finance\.taxProjectedPayCountOther .*"count":"16"/)).toBeTruthy()
    // A state's lines are named for it; a figure that is zero is left out.
    expect(within(projected).getByText('finance.taxStateIncomeTax {"state":"CA"}')).toBeTruthy()
    expect(within(projected).queryByText('finance.taxRetirementRoth')).toBeNull()
    expect(within(projected).queryByText('finance.taxComparisonTitle')).toBeNull()

    const fromW2 = panel('finance.taxEmploymentTitle {"employee":"Sam Example","employer":"Sample Works Inc"}')
    expect(within(fromW2).getByText('finance.taxFromW2')).toBeTruthy()
    expect(within(fromW2).queryByText('finance.taxByDecember31')).toBeNull()
    expect(within(fromW2).getByText('finance.taxComparisonTitle')).toBeTruthy()
    const matchingRow = within(fromW2).getByText('Wages, tips, other compensation').closest('tr')!
    expect(within(matchingRow).getByText('finance.taxMatches')).toBeTruthy()
    const differingRow = within(fromW2).getByText('Federal income tax withheld').closest('tr')!
    expect(within(differingRow).getByText(/^finance\.taxDiffersBy .*\$12\.00/)).toBeTruthy()

    // Two employments, so the household's totals too, and each person's.
    const household = panel('finance.taxHouseholdTitle')
    expect(within(household).getByText('Pat Example')).toBeTruthy()
    expect(within(household).getByText('Sam Example')).toBeTruthy()

    // The W-2s and the paystubs, each with its check.
    const w2Row = within(panel('finance.taxFormsTitle')).getByText('Sample Works Inc').closest('tr')!
    expect(within(w2Row).getByText('finance.taxFormAgrees')).toBeTruthy()
    expect(
      within(w2Row)
        .getByRole('link', { name: /finance\.openTaxDocumentFile/ })
        .getAttribute('href'),
    ).toBe('/api/v1/agent/attachments/attachment-w2')
    const paystubRow = (await within(panel('finance.paystubsTitle')).findByText('Example Employer LLC')).closest('tr')!
    expect(within(paystubRow).getByText('finance.receiptBalanced')).toBeTruthy()
    expect(within(paystubRow).getByText('$2,720.00')).toBeTruthy()
    expect(calledWith('FinancePaystubs')[0]).toEqual({ taxYear: 2026, limit: 50, offset: 0 })
  })

  it('says to add paystubs or W-2s when the year has none, and nothing else', async () => {
    answer({
      summary: () => ({
        ...yearSummary,
        taxYear: 2025,
        paystubCount: 0,
        taxFormCount: 0,
        taxYearEmployments: [],
        taxYearPeople: [],
        taxYearFindings: [],
      }),
      paystubs: () => [],
      taxForms: () => [],
    })
    renderSection('/finance/taxes?year=2025')
    expect(await screen.findByText('finance.noTaxDocuments {"year":"2025"}')).toBeTruthy()
    expect(screen.queryByRole('heading', { name: 'finance.paystubsTitle' })).toBeNull()
    expect(screen.queryByRole('heading', { name: 'finance.taxFindingsTitle' })).toBeNull()
  })

  it('opens the paystub a finding is about, read by its identifier when it is not on the page', async () => {
    answer()
    renderSection()
    const findings = await waitFor(() => panel('finance.taxFindingsTitle'))
    expect(within(findings).getByText('finance.taxFindingPayDateGap')).toBeTruthy()
    fireEvent.click(within(findings).getByRole('button', { name: /Four weeks pass/ }))
    const dialog = await screen.findByRole('alertdialog', { name: 'Example Employer LLC' })
    expect(calledWith('FinancePaystub')).toContainEqual({ paystubId: 'paystub-september' })
    expect(within(dialog).getByText(/finance\.paystubPaidOn/)).toBeTruthy()
  })

  it('shows a paystub its lines, grouped, and deletes it after asking', async () => {
    answer()
    renderSection()
    const paystubRow = (
      await within(await waitFor(() => panel('finance.paystubsTitle'))).findByText('Example Employer LLC')
    ).closest('tr')!
    fireEvent.click(paystubRow)
    const dialog = await screen.findByRole('alertdialog', { name: 'Example Employer LLC' })
    expect(within(dialog).getByText('finance.paystubEarnings')).toBeTruthy()
    expect(within(dialog).getByText('Regular')).toBeTruthy()
    // Taxes carry the wages they were figured on when printed.
    expect(within(dialog).getByText('finance.paystubWagesThisPeriod')).toBeTruthy()
    expect(within(dialog).getByText('Federal Income Tax')).toBeTruthy()
    // Deductions have an employer's column because one line prints it.
    expect(within(dialog).getByText('finance.paystubEmployerPart')).toBeTruthy()
    expect(within(dialog).getByText('$25.00')).toBeTruthy()
    const netLine = within(dialog).getByText('finance.paystubNet').closest('p')!
    expect(within(netLine).getByText('$2,720.00')).toBeTruthy()

    fireEvent.click(within(dialog).getByRole('button', { name: 'finance.deletePaystub' }))
    const confirmation = await screen.findByRole('alertdialog', { name: /^finance\.deletePaystubTitle/ })
    expect(calledWith('DeletePaystub')).toEqual([])
    fireEvent.click(within(confirmation).getByRole('button', { name: 'common.delete' }))
    await waitFor(() => expect(calledWith('DeletePaystub')).toEqual([{ paystubId: 'paystub-october' }]))
    await waitFor(() => expect(toast.done).toHaveBeenCalledWith(expect.stringMatching(/^finance\.paystubDeleted/)))
  })

  it('opens a W-2 with its boxes and deletes it after asking', async () => {
    answer()
    renderSection()
    const w2Panel = await waitFor(() => panel('finance.taxFormsTitle'))
    fireEvent.click(within(w2Panel).getByRole('button', { name: /^finance\.taxFormDetailsOf/ }))
    const dialog = await screen.findByRole('alertdialog', { name: /^finance\.taxFormTitle/ })
    expect(within(dialog).getByText('finance.w2Box1')).toBeTruthy()
    expect(within(dialog).getByText('D')).toBeTruthy()
    expect(within(dialog).getByText('$50,000.00')).toBeTruthy()
    expect(within(dialog).getByText('Social Security tax is 6.2% of Social Security wages.')).toBeTruthy()
    fireEvent.click(within(dialog).getByRole('button', { name: 'finance.deleteTaxForm' }))
    const confirmation = await screen.findByRole('alertdialog', { name: /^finance\.deleteTaxFormTitle/ })
    fireEvent.click(within(confirmation).getByRole('button', { name: 'common.delete' }))
    await waitFor(() => expect(calledWith('DeleteTaxForm')).toEqual([{ taxFormId: 'tax-form-sample' }]))
  })

  it('uploads several files as tax documents, has each read, and says when each is read', async () => {
    let isRead = false
    const uploadedPaystub = { ...octoberPaystub, id: 'paystub-uploaded', agentAttachmentId: 'attachment-first' }
    const uploadedW2 = {
      ...sampleWorksW2,
      financeTaxForm: {
        ...sampleWorksW2.financeTaxForm,
        id: 'tax-form-uploaded',
        agentAttachmentId: 'attachment-second',
        taxYear: 2025,
      },
    }
    answer({
      paystubs: () => (isRead ? [uploadedPaystub, octoberPaystub] : [octoberPaystub]),
      taxForms: () => (isRead ? [uploadedW2, sampleWorksW2] : [sampleWorksW2]),
    })
    upload.mockReturnValue({
      promise: Promise.resolve({ attachments: [{ id: 'attachment-first' }, { id: 'attachment-second' }] }),
      cancel: vi.fn(),
    })
    renderSection()
    await waitFor(() => panel('finance.paystubsTitle'))
    const stub = new File(['%PDF-'], 'stub.pdf', { type: 'application/pdf' })
    const w2 = new File(['picture'], 'w2.jpg', { type: 'image/jpeg' })
    fireEvent.change(screen.getByTestId('tax-document-files'), { target: { files: [stub, w2] } })
    await waitFor(() =>
      expect(upload).toHaveBeenCalledWith(
        'POST',
        '/api/v1/agent/attachments?isTaxDocument=true',
        [stub, w2],
        expect.any(Function),
      ),
    )
    await waitFor(() =>
      expect(calledWith('ReadTaxDocument')).toEqual([
        { agentAttachmentId: 'attachment-first' },
        { agentAttachmentId: 'attachment-second' },
      ]),
    )
    expect(toast.done).toHaveBeenCalledWith('finance.taxDocumentReadingOther {"count":"2"}')
    isRead = true
    const toastMessages = () => toast.done.mock.calls.map(([message]) => String(message))
    await waitFor(() =>
      expect(toastMessages().some((message) => message.startsWith('finance.paystubRead '))).toBe(true),
    )
    expect(toastMessages().some((message) => message.startsWith('finance.taxFormRead '))).toBe(true)
    // A W-2 for another year than the one shown offers to show its year.
    const w2Toast = toast.done.mock.calls.find(([message]) => String(message).startsWith('finance.taxFormRead'))!
    expect(w2Toast[1]?.label).toBe('finance.taxShowYear {"year":"2025"}')
    w2Toast[1]?.run()
    await waitFor(() => expect(shownSearch).toBe('?year=2025'))
  })

  it('offers this year and the four before it, and the year shown when it is older', () => {
    expect(offeredYears(2026, 2026)).toEqual(['2026', '2025', '2024', '2023', '2022'])
    expect(offeredYears(2026, 2019)).toEqual(['2026', '2025', '2024', '2023', '2022', '2019'])
  })
})
