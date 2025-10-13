import { useQuery } from '@tanstack/react-query';
import { CardGrid } from '../components/card-grid';
import { PageLayout } from '../components/page-layout';
// import { TablePane } from '../components/table-pane'
// import {
//   Table,
//   TableBody,
//   TableCell,
//   TableHead,
//   TableHeader,
//   TableRow,
// } from '../components/ui/table'
import { api } from '../lib/api';

type RackInfo = {
  name?: string;
  domain?: string;
  provider?: string;
  region?: string;
  status?: string;
  type?: string;
  version?: string;
  count?: number;
  'rack-domain'?: string;
  outputs?: Record<string, string>;
  parameters?: Record<string, string>;
};

export function RackPage() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['rack-info'],
    queryFn: async () => {
      const res = await api.get<RackInfo>('/api/v1/rack');
      return res;
    },
  });
  // Parameters come from the rack info response (/system)

  return (
    <PageLayout
      description="Overview, parameters, and outputs for the selected rack"
      title="Rack"
    >
      {error && (
        <div className="mb-4 rounded-md border border-destructive/50 bg-destructive/10 p-3 text-destructive text-sm">
          {(error as Error).message}
        </div>
      )}
      {isLoading && (
        <div className="text-muted-foreground">Loading rack info…</div>
      )}
      {data && (
        <div className="space-y-8">
          <CardGrid
            items={[
              { label: 'Name', value: data.name },
              { label: 'Domain', value: data.domain },
              { label: 'Rack Domain', value: data['rack-domain'] },
              { label: 'Provider', value: data.provider },
              { label: 'Region', value: data.region },
              { label: 'Type', value: data.type },
              { label: 'Version', value: data.version },
              { label: 'Count', value: data.count ?? '' },
              { label: 'Status', value: data.status },
            ]}
          />

          {/* Update - Racks don't actually return parameters, those are in Terraform. */}
          {/* {(() => {
            const p = data.parameters || {}
            return p && Object.keys(p).length > 0 ? (
              <TablePane empty={false} emptyMessage="No parameters found" title="Rack Parameters">
                <KvTable obj={p} />
              </TablePane>
            ) : null
          })()} */}

          {/* Outputs intentionally hidden */}
        </div>
      )}
    </PageLayout>
  );
}

// function KvTable({ obj }: { obj: Record<string, string> }) {
//   const entries = Object.entries(obj).sort((a, b) => a[0].localeCompare(b[0]))
//   return (
//     <div className="overflow-x-auto">
//       <Table>
//         <TableHeader>
//           <TableRow>
//             <TableHead>Key</TableHead>
//             <TableHead>Value</TableHead>
//           </TableRow>
//         </TableHeader>
//         <TableBody>
//           {entries.map(([k, v]) => (
//             <TableRow key={k}>
//               <TableCell className="font-mono text-xs">{k}</TableCell>
//               <TableCell className="truncate font-mono text-xs">{v}</TableCell>
//             </TableRow>
//           ))}
//         </TableBody>
//       </Table>
//     </div>
//   )
// }
