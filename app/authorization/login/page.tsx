import Header from "@/components/header/Header";
import Authorization from "@/components/authorization/Authorization";

export const metadata = {
  title: "Blob | Login",
};

const Login = () => {
  return (
    <section style={{ height: "calc(100vh - 105px)" }}>
      <Header isAuthorization className="display: 'flex';" />
      <Authorization isLogin />
    </section>
  );
};

export default Login;
