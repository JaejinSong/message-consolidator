import { TokenUsage } from '../types';

/**
 * TokenUsageCard components renders AI API consumption metrics
 * within the Insights dashboard.
 */
export class TokenUsageCard {
    private container: HTMLElement | null;
    private numberFormatter: Intl.NumberFormat;
    private currencyFormatter: Intl.NumberFormat;

    constructor(containerId: string = 'tokenUsageContainer') {
        this.container = document.getElementById(containerId);
        // Using undefined locale to follow browser language or default
        this.numberFormatter = new Intl.NumberFormat(undefined, { 
            maximumFractionDigits: 0 
        });
        this.currencyFormatter = new Intl.NumberFormat('en-US', { 
            style: 'currency', 
            currency: 'USD', 
            minimumFractionDigits: 4 
        });
    }

    /**
     * Renders the token usage cards into the container.
     * @param data TokenUsage data from the API
     */
    public render(data: TokenUsage): void {
        if (!this.container) return;

        // Numeric safety and guard clauses
        const todayTotal = Number(data.todayTotal) || 0;
        const todayPrompt = Number(data.todayPrompt) || 0;
        const todayCompletion = Number(data.todayCompletion) || 0;
        const monthlyTotal = Number(data.monthlyTotal) || 0;
        const monthlyCost = Number(data.monthlyCost) || 0;
        const modelName = data.model || 'Gemini 1.5 Flash';

        // Get labels from global i18n if available, else fallback
        const i18n = (window as any).i18n;
        const labelToday = i18n?.t('tokenMenuTitle') || 'Today\'s Consumption';
        const labelMonthly = i18n?.t('tokenUsed') || 'Monthly Total';
        const labelIn = 'IN';
        const labelOut = 'OUT';
        const labelEstCost = 'Est. Cost';

        this.container.innerHTML = `
            <div class="c-token-usage">
                <div class="c-token-usage__card">
                    <span class="c-token-usage__label">${labelToday}</span>
                    <div class="c-token-usage__value">${this.numberFormatter.format(todayTotal)}</div>
                    <div class="c-token-usage__footer">
                        <span class="c-token-usage__subvalue">
                            ${this.numberFormatter.format(todayPrompt)} ${labelIn} / ${this.numberFormatter.format(todayCompletion)} ${labelOut}
                        </span>
                        <span class="c-token-usage__model-badge">${modelName}</span>
                    </div>
                </div>
                <div class="c-token-usage__card">
                    <span class="c-token-usage__label">${labelMonthly}</span>
                    <div class="c-token-usage__value">${this.numberFormatter.format(monthlyTotal)}</div>
                    <div class="c-token-usage__footer">
                        <span class="c-token-usage__subvalue">${labelEstCost}: ${this.currencyFormatter.format(monthlyCost)}</span>
                    </div>
                </div>
            </div>
        `;
    }

    /**
     * Cleanup and memory management.
     */
    public destroy(): void {
        if (this.container) {
            this.container.innerHTML = '';
        }
        this.container = null;
    }
}
