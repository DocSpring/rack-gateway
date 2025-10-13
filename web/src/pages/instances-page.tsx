import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { PageLayout } from '../components/page-layout';
import { TablePane } from '../components/table-pane';
import { Button } from '../components/ui/button';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '../components/ui/table';
import { api } from '../lib/api';
import { DEFAULT_PER_PAGE } from '../lib/constants';

type Instance = {
  id: string;
  status: string;
  private_ip?: string;
  public_ip?: string;
  instance_type?: string;
};

export function InstancesPage() {
  type RackInfo = { provider?: string; region?: string };
  const { data: rack } = useQuery({
    queryKey: ['rack-info'],
    queryFn: async () => api.get<RackInfo>('/api/v1/rack'),
    staleTime: Number.POSITIVE_INFINITY,
    gcTime: Number.POSITIVE_INFINITY,
  });
  const {
    data = [],
    isLoading,
    error,
  } = useQuery({
    queryKey: ['instances'],
    queryFn: async () => api.get<Instance[]>('/api/v1/convox/instances'),
    refetchOnMount: 'always',
    refetchOnWindowFocus: true,
    staleTime: 0,
  });
  const perPage = DEFAULT_PER_PAGE;
  const total = data.length;
  const totalPages = Math.max(1, Math.ceil(total / perPage));
  const [page, setPage] = useState(1);
  const start = (page - 1) * perPage;
  const end = Math.min(start + perPage, total);
  const rows = data.slice(start, end);

  return (
    <PageLayout
      description="Rack instances across the cluster"
      title="Instances"
    >
      <TablePane
        empty={total === 0}
        emptyMessage="No instances found"
        error={error ? (error as Error).message : null}
        loading={isLoading}
      >
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>ID</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Private IP</TableHead>
              <TableHead>Public IP</TableHead>
              <TableHead>Type</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((i) => (
              <TableRow key={i.id}>
                <TableCell className="font-mono text-xs">
                  {(() => {
                    const isAws =
                      (rack?.provider || '').toLowerCase() === 'aws';
                    const region = rack?.region || '';
                    const href = isAws
                      ? `https://console.aws.amazon.com/ec2/v2/home?region=${region}#InstanceDetails:instanceId=${i.id}`
                      : '';
                    return isAws && region ? (
                      <a
                        className="underline hover:no-underline"
                        href={href}
                        rel="noreferrer noopener"
                        target="_blank"
                        title={`Open ${i.id} in AWS Console`}
                      >
                        {i.id}
                      </a>
                    ) : (
                      i.id
                    );
                  })()}
                </TableCell>
                <TableCell>{i.status}</TableCell>
                <TableCell>{i.private_ip || '—'}</TableCell>
                <TableCell>{i.public_ip || '—'}</TableCell>
                <TableCell>{i.instance_type || '—'}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>

        {total > 0 && (
          <div className="mt-4 flex items-center justify-between">
            <div className="text-muted-foreground text-sm">
              Showing {start + 1}–{end} of {total} instances
            </div>
            <div className="flex gap-2">
              <Button
                disabled={page === 1}
                onClick={() => setPage((p) => Math.max(1, p - 1))}
                variant="outline"
              >
                Previous
              </Button>
              <Button
                disabled={page === totalPages}
                onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
                variant="outline"
              >
                Next
              </Button>
            </div>
          </div>
        )}
      </TablePane>
    </PageLayout>
  );
}
