import { graphqlFetch } from '@/services/graphqlClient';

export interface StatisticsMetrics {
  createdCards: number;
  closedCards: number;
  filesChanged: number;
  totalTokens: number;
}

export interface StatisticsProjectMetrics {
  key: string;
  label: string;
  metrics: StatisticsMetrics;
}

export interface StatisticsTimelineBucket {
  key: string;
  label: string;
  projects: StatisticsProjectMetrics[];
}

export interface StatisticsDashboard {
  today: StatisticsMetrics;
  total: StatisticsMetrics;
  byDay: StatisticsTimelineBucket[];
}

export interface StatisticsRange {
  startDate: string;
  endDate: string;
}

export async function getStatistics(range: StatisticsRange): Promise<StatisticsDashboard> {
  const data = await graphqlFetch<{ statistics: StatisticsDashboard }>({
    query: `
      query Statistics($input: StatisticsQueryInput!) {
        statistics(input: $input) {
          today {
            createdCards
            closedCards
            filesChanged
            totalTokens
          }
          total {
            createdCards
            closedCards
            filesChanged
            totalTokens
          }
          byDay {
            key
            label
            projects {
              key
              label
              metrics {
                createdCards
                closedCards
                filesChanged
                totalTokens
              }
            }
          }
        }
      }
    `,
    variables: { input: range },
  });
  return data.statistics;
}
