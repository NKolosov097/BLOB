import Header from "@/components/header/Header";
import Authorization from "@/components/authorization/Authorization";

export const metadata = {
  title: "Blob | Recover account",
};

const Recover = () => {
  return (
    <section style={{ height: "calc(100vh - 105px)" }}>
      <Header isAuthorization className="display: 'flex';" />
      <Authorization isRecover />
    </section>
  );
};

export default Recover;
