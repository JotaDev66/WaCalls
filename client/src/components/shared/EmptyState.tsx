import * as React from "react";
import { Card, CardContent } from "@/components/ui/card";

type Props = {
  icon?: React.ReactNode;
  title: string;
  description?: string;
};

export const EmptyState = ({ icon, title, description }: Props) => (
  <Card className="border-dashed bg-transparent shadow-none">
    <CardContent className="flex flex-col items-center justify-center gap-2 py-10 text-center">
      {icon && (
        <div className="flex h-12 w-12 items-center justify-center rounded-full bg-muted text-muted-foreground">
          {icon}
        </div>
      )}
      <p className="font-medium">{title}</p>
      {description && (
        <p className="max-w-xs text-sm text-muted-foreground">{description}</p>
      )}
    </CardContent>
  </Card>
);
