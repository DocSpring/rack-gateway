import type { ReactNode } from 'react';

export function PageLayout({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children: ReactNode;
}) {
  return (
    <div className="p-8">
      <div className="mb-8">
        <h1 className="font-bold text-3xl">{title}</h1>
        {description ? (
          <p className="mt-2 text-muted-foreground">{description}</p>
        ) : null}
      </div>
      {children}
    </div>
  );
}
