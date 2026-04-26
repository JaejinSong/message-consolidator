import { TokenUsage } from '../types';
import { t } from '../i18n';
import { state } from '../state';

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
     * @param data TokenUsage data from the backend (already calculated)
     */
    public render(data: TokenUsage): void {
        if (!this.container) return;

        // Extract pre-calculated values from backend
        const todayTotal = data.todayTotal;
        const todayPrompt = data.todayPrompt;
        const todayCompletion = data.todayCompletion;
        const monthlyTotal = data.monthlyTotal;
        const monthlyCost = data.monthlyCost;
        const modelName = data.model;

        const lang = state.currentLang || 'en';
        const labelToday = t('tokenMenuTitle', lang) || 'Today\'s Consumption';
        const labelMonthly = t('tokenUsed', lang) || 'Monthly Total';
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
