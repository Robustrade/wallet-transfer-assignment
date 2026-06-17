import { DatePipe, DecimalPipe } from '@angular/common';
import { Component, computed, input, signal } from '@angular/core';
import { Transfer } from '../models/wallet.models';

export type ChartRange = 'today' | 'yesterday' | '30d' | '90d';

interface ChartRangeOption {
  id: ChartRange;
  label: string;
}

export interface DayBarStat {
  date: string;
  processed: number;
  failed: number;
  pending: number;
  volume: number;
}

@Component({
  selector: 'app-chart-panel',
  imports: [DecimalPipe, DatePipe],
  templateUrl: './chart-panel.html',
  styleUrl: './chart-panel.css',
})
export class ChartPanel {
  readonly title = input('Transaction analytics');
  readonly transfers = input<Transfer[]>([]);
  readonly compact = input(false);

  readonly rangeOptions: ChartRangeOption[] = [
    { id: 'today', label: 'Today' },
    { id: 'yesterday', label: 'Yesterday' },
    { id: '30d', label: '30 Days' },
    { id: '90d', label: '90 Days' },
  ];

  readonly chartRange = signal<ChartRange>('30d');
  readonly hoveredDate = signal<string | null>(null);

  readonly allDayStats = computed(() => this.buildDayStats(this.transfers(), 90));

  readonly filteredStats = computed(() => {
    const all = this.allDayStats();
    const range = this.chartRange();
    const today = this.utcDateString(0);
    const yesterday = this.utcDateString(1);

    switch (range) {
      case 'today':
        return all.filter((stat) => stat.date === today);
      case 'yesterday':
        return all.filter((stat) => stat.date === yesterday);
      case '30d':
        return all.slice(-30);
      case '90d':
      default:
        return all;
    }
  });

  readonly summary = computed(() => {
    const stats = this.filteredStats();
    return {
      processed: stats.reduce((sum, stat) => sum + stat.processed, 0),
      failed: stats.reduce((sum, stat) => sum + stat.failed, 0),
      pending: stats.reduce((sum, stat) => sum + stat.pending, 0),
      volume: stats.reduce((sum, stat) => sum + stat.volume, 0),
      total: stats.reduce((sum, stat) => sum + stat.processed + stat.failed + stat.pending, 0),
      activeDays: stats.filter((stat) => stat.processed + stat.failed + stat.pending > 0).length,
    };
  });

  readonly maxBarValue = computed(() => {
    const stats = this.filteredStats();
    return Math.max(...stats.map((stat) => Math.max(stat.processed, stat.failed, 1)), 1);
  });

  readonly dateRangeLabel = computed(() => {
    const stats = this.filteredStats();
    if (stats.length === 0) {
      return 'No data';
    }
    if (stats.length === 1) {
      return stats[0].date;
    }
    return `${stats[0].date} → ${stats[stats.length - 1].date}`;
  });

  readonly hoveredStat = computed(() => {
    const date = this.hoveredDate();
    if (!date) {
      return null;
    }
    return this.filteredStats().find((stat) => stat.date === date) ?? null;
  });

  setRange(range: ChartRange): void {
    this.chartRange.set(range);
    this.hoveredDate.set(null);
  }

  barHeight(value: number): number {
    return Math.max(value > 0 ? 10 : 4, Math.round((value / this.maxBarValue()) * (this.compact() ? 120 : 160)));
  }

  showXLabel(index: number, total: number): boolean {
    const range = this.chartRange();
    if (total <= 1) {
      return true;
    }
    if (range === 'today' || range === 'yesterday') {
      return true;
    }
    if (range === '30d') {
      return index === 0 || index === total - 1 || index % 5 === 0;
    }
    return index === 0 || index === total - 1 || index % 10 === 0;
  }

  private buildDayStats(transfers: Transfer[], days: number): DayBarStat[] {
    const result: DayBarStat[] = [];
    const grouped = new Map<string, DayBarStat>();

    for (let offset = days - 1; offset >= 0; offset--) {
      const date = this.utcDateString(offset);
      grouped.set(date, { date, processed: 0, failed: 0, pending: 0, volume: 0 });
    }

    for (const transfer of transfers) {
      const date = transfer.createdAt.slice(0, 10);
      const stat = grouped.get(date);
      if (!stat) {
        continue;
      }
      if (transfer.status === 'PROCESSED') {
        stat.processed++;
        stat.volume += transfer.amount;
      } else if (transfer.status === 'FAILED') {
        stat.failed++;
      } else {
        stat.pending++;
      }
    }

    for (const stat of grouped.values()) {
      result.push(stat);
    }
    return result;
  }

  private utcDateString(offsetDays: number): string {
    const date = new Date();
    date.setUTCDate(date.getUTCDate() - offsetDays);
    return date.toISOString().slice(0, 10);
  }
}
