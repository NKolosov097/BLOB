import "./globals.css";
import { Poppins } from "next/font/google";

const font = Poppins({ subsets: ["latin"], weight: "500" });

export const metadata = {
  title: "Blob",
  description: "Social network Blob",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en">
      <body className={font.className}>{children}</body>
    </html>
  );
}
