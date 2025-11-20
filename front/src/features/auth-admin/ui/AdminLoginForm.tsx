// src/features/auth-admin/ui/AdminLoginForm.tsx
import { useEffect } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { zodResolver } from "@hookform/resolvers/zod";
import { useNavigate, useLocation } from "react-router-dom";

import { useAdminLoginMutation } from "../model/useAdminLoginMutation";
import { useAdminAuthStore } from "../model/useAdminAuth";

import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";

const schema = z.object({
  username: z.string().min(1, "Обязательное поле"),
  password: z.string().min(1, "Обязательное поле"),
});

type FormValues = z.infer<typeof schema>;

export function AdminLoginForm() {
  const navigate = useNavigate();
  const location = useLocation();

  const { mutateAsync, isPending } = useAdminLoginMutation();
  const authError = useAdminAuthStore((s) => s.error);
  const isAuthenticated = useAdminAuthStore((s) => s.isAuthenticated);

  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<FormValues>({
    resolver: zodResolver(schema),
  });

  useEffect(() => {
    if (isAuthenticated) {
      const from =
        (location.state as any)?.from?.pathname || "/admin/scoreboard";
      navigate(from, { replace: true });
    }
  }, [isAuthenticated, navigate, location.state]);

  const onSubmit = async (values: FormValues) => {
    await mutateAsync(values);
  };

  return (
    <Card className="w-full max-w-md border-slate-800 bg-slate-950/80 shadow-xl shadow-indigo-900/40 backdrop-blur">
      <CardHeader>
        <CardTitle className="text-sm uppercase tracking-[0.3em] text-slate-300">
          Admin access
        </CardTitle>
        <CardDescription className="text-slate-400">
          Войдите в панель управления FinalSibCTF2025.
        </CardDescription>
      </CardHeader>

      <form onSubmit={handleSubmit(onSubmit)}>
        <CardContent className="space-y-4 text-white">
          {authError && (
            <div className="rounded-md border border-red-500/40 bg-red-500/10 px-3 py-2 text-xs text-red-200">
              {authError}
            </div>
          )}

          <div className="space-y-2">
            <Label htmlFor="username">User</Label>
            <Input
              id="username"
              autoComplete="username"
              {...register("username")}
            />
            {errors.username && (
              <p className="text-xs text-red-400">{errors.username.message}</p>
            )}
          </div>

          <div className="space-y-2">
            <Label htmlFor="password">Password</Label>
            <Input
              id="password"
              type="password"
              autoComplete="current-password"
              {...register("password")}
            />
            {errors.password && (
              <p className="text-xs text-red-400">{errors.password.message}</p>
            )}
          </div>
        </CardContent>

        <CardFooter className="flex justify-end">
          <Button type="submit" className="px-6" disabled={isPending}>
            {isPending ? "Entering…" : "Enter"}
          </Button>
        </CardFooter>
      </form>
    </Card>
  );
}
